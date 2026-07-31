package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lgforsberg/bifrost/mail"
)

type Defaults struct {
	QuoteReplies bool   `json:"quoteReplies"`
	PeekOnRead   bool   `json:"peekOnRead"`
	SaveToSent   bool   `json:"saveToSent"`
	SentFolder   string `json:"sentFolder"`
	DraftsFolder string `json:"draftsFolder"`
}

type accountJSON struct {
	Address      string     `json:"address"`
	DisplayName  string     `json:"displayName"`
	Default      bool       `json:"default"`
	IMAP         serverJSON `json:"imap"`
	SMTP         serverJSON `json:"smtp"`
	Password     string     `json:"password"`
	PasswordFile string     `json:"passwordFile"`
}

type serverJSON struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`
}

type configJSON struct {
	Defaults Defaults      `json:"defaults"`
	Accounts []accountJSON `json:"accounts"`
}

type Config struct {
	Defaults       Defaults
	Accounts       []mail.AccountConfig
	defaultAccount int // index of account with "default": true, or -1
}

func Load(path string) (*Config, error) {
	cfgPath := resolvePath(path)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found at %s: %w", cfgPath, mail.ErrInvalidConfig)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config JSON: %w", err)
	}

	// Apply defaults for boolean fields that should default to true.
	// We need to distinguish "field absent" from "field set to false".
	defaultsPresent := parseDefaultsKeys(data)
	if !raw.Defaults.QuoteReplies && !defaultsPresent["quoteReplies"] {
		raw.Defaults.QuoteReplies = true
	}
	if !raw.Defaults.SaveToSent && !defaultsPresent["saveToSent"] {
		raw.Defaults.SaveToSent = true
	}

	cfg := &Config{Defaults: raw.Defaults, defaultAccount: -1}

	for i, acct := range raw.Accounts {
		password, err := resolvePassword(acct.Password, acct.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("account %d (%s): %w", i, acct.Address, err)
		}

		if acct.Address == "" {
			return nil, fmt.Errorf("account %d: address is required: %w", i, mail.ErrInvalidConfig)
		}
		if acct.IMAP.Host == "" {
			return nil, fmt.Errorf("account %d (%s): imap.host is required: %w", i, acct.Address, mail.ErrInvalidConfig)
		}
		if acct.SMTP.Host == "" {
			return nil, fmt.Errorf("account %d (%s): smtp.host is required: %w", i, acct.Address, mail.ErrInvalidConfig)
		}

		cfg.Accounts = append(cfg.Accounts, mail.AccountConfig{
			Address:        acct.Address,
			DisplayName:    acct.DisplayName,
			IMAPHost:       acct.IMAP.Host,
			IMAPPort:       withDefault(acct.IMAP.Port, 993),
			IMAPEncryption: withDefaultStr(acct.IMAP.Encryption, "tls"),
			SMTPHost:       acct.SMTP.Host,
			SMTPPort:       withDefault(acct.SMTP.Port, 587),
			SMTPEncryption: withDefaultStr(acct.SMTP.Encryption, "starttls"),
			Username:       acct.Address,
			Password:       password,
		})
		if acct.Default && cfg.defaultAccount == -1 {
			cfg.defaultAccount = len(cfg.Accounts) - 1
		}
	}

	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured: %w", mail.ErrInvalidConfig)
	}

	return cfg, nil
}

func DefaultAccount(cfg *Config) (*mail.AccountConfig, error) {
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured: %w", mail.ErrInvalidConfig)
	}
	if cfg.defaultAccount >= 0 && cfg.defaultAccount < len(cfg.Accounts) {
		return &cfg.Accounts[cfg.defaultAccount], nil
	}
	return &cfg.Accounts[0], nil
}

func AccountByAddress(cfg *Config, addr string) (*mail.AccountConfig, error) {
	normalized := mail.NormalizeAddressLower(addr)
	addrLower := strings.ToLower(addr)

	// Exact match (case-insensitive, plus-tag normalized)
	for i := range cfg.Accounts {
		if mail.NormalizeAddressLower(cfg.Accounts[i].Address) == normalized {
			return &cfg.Accounts[i], nil
		}
	}

	// Substring match (case-insensitive) — matches on local part or full address
	var matches []*mail.AccountConfig
	for i := range cfg.Accounts {
		acctLower := strings.ToLower(cfg.Accounts[i].Address)
		if strings.Contains(acctLower, addrLower) {
			matches = append(matches, &cfg.Accounts[i])
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Address
		}
		return nil, fmt.Errorf("account %q is ambiguous, matches: %s: %w", addr, strings.Join(names, ", "), mail.ErrInvalidConfig)
	}

	return nil, fmt.Errorf("account %q not found: %w", addr, mail.ErrNotFound)
}

// IsDefaultAccount returns true if the account at the given index is the configured default.
func IsDefaultAccount(cfg *Config, idx int) bool {
	if cfg.defaultAccount >= 0 {
		return idx == cfg.defaultAccount
	}
	return idx == 0
}

func resolvePath(override string) string {
	if override != "" {
		return expandTilde(override)
	}
	if env := os.Getenv("BIFROST_CONFIG"); env != "" {
		return expandTilde(env)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bifrost", "config.json")
}

func resolvePassword(inline, file string) (string, error) {
	if file != "" {
		path := expandTilde(file)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading password file %s: %w: %w", path, err, mail.ErrInvalidConfig)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if inline != "" {
		return inline, nil
	}
	return "", fmt.Errorf("password or passwordFile is required: %w", mail.ErrInvalidConfig)
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func withDefault(val, def int) int {
	if val == 0 {
		return def
	}
	return val
}

func withDefaultStr(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// parseDefaultsKeys returns which keys are present in the "defaults" JSON object.
func parseDefaultsKeys(data []byte) map[string]bool {
	var outer struct {
		Defaults map[string]json.RawMessage `json:"defaults"`
	}
	if err := json.Unmarshal(data, &outer); err != nil {
		return nil
	}
	present := make(map[string]bool, len(outer.Defaults))
	for k := range outer.Defaults {
		present[k] = true
	}
	return present
}

// DefaultConfigPath returns the standard config file location.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bifrost", "config.json")
}

// TemplateJSON returns a config template for `config init`.
func TemplateJSON() string {
	return `{
  "defaults": {
    "quoteReplies": true,
    "peekOnRead": false,
    "saveToSent": true,
    "sentFolder": "",
    "draftsFolder": ""
  },
  "accounts": [
    {
      "address": "you@example.com",
      "displayName": "Your Name",
      "default": true,
      "imap": { "host": "imap.example.com", "port": 993, "encryption": "tls" },
      "smtp": { "host": "smtp.example.com", "port": 587, "encryption": "starttls" },
      "password": "",
      "passwordFile": "~/.bifrost/pass-you@example.com"
    }
  ]
}
`
}
