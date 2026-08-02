package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lgforsberg/bifrost/mail"
)

type Defaults struct {
	QuoteReplies  bool   `json:"quoteReplies"`
	PeekOnRead    bool   `json:"peekOnRead"`
	SaveToSent    bool   `json:"saveToSent"`
	SentFolder    string `json:"sentFolder"`
	DraftsFolder  string `json:"draftsFolder"`
	TrashFolder   string `json:"trashFolder"`
	ArchiveFolder string `json:"archiveFolder"`

	// Timeout is a Go duration such as "10s". Empty leaves the built-in
	// network timeouts alone.
	Timeout string `json:"timeout"`
}

type accountJSON struct {
	Address      string     `json:"address"`
	DisplayName  string     `json:"displayName"`
	Default      bool       `json:"default"`
	IMAP         serverJSON `json:"imap"`
	SMTP         serverJSON `json:"smtp"`
	Password     string     `json:"password"`
	PasswordFile string     `json:"passwordFile"`

	// AuthMechanism and TokenCommand go together: the first says how to
	// authenticate, the second says what to run to get the token. They are
	// per-account rather than defaulted, as passwords are, because a token
	// belongs to one mailbox.
	AuthMechanism string   `json:"authMechanism"`
	TokenCommand  []string `json:"tokenCommand"`

	// Folder overrides, which fall back to the ones in defaults. They belong
	// here as well because two accounts on different providers rarely name
	// their folders the same way.
	SentFolder    string `json:"sentFolder"`
	DraftsFolder  string `json:"draftsFolder"`
	TrashFolder   string `json:"trashFolder"`
	ArchiveFolder string `json:"archiveFolder"`

	// Timeout overrides the default for this account, for the one server on
	// a slow link among several that are not.
	Timeout string `json:"timeout"`
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
		if err := validateAuth(acct); err != nil {
			return nil, fmt.Errorf("account %d (%s): %w", i, acct.Address, err)
		}

		// Only one of the two is needed, and which one is settled above.
		var password string
		if acct.AuthMechanism == "" {
			resolved, err := resolvePassword(acct.Password, acct.PasswordFile)
			if err != nil {
				return nil, fmt.Errorf("account %d (%s): %w", i, acct.Address, err)
			}
			password = resolved
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

		timeout, err := parseTimeout(withDefaultStr(acct.Timeout, raw.Defaults.Timeout))
		if err != nil {
			return nil, fmt.Errorf("account %d (%s): %w", i, acct.Address, err)
		}

		// The helper inherits the account's network timeout, on the grounds
		// that someone who said how long to wait for a server meant how long
		// to wait for anything.
		var tokenSource mail.TokenSource
		if len(acct.TokenCommand) > 0 {
			tokenSource = tokenCommandSource(acct.TokenCommand, timeout)
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
			AuthMechanism:  strings.ToLower(strings.TrimSpace(acct.AuthMechanism)),
			TokenSource:    tokenSource,
			SentFolder:     withDefaultStr(acct.SentFolder, raw.Defaults.SentFolder),
			DraftsFolder:   withDefaultStr(acct.DraftsFolder, raw.Defaults.DraftsFolder),
			TrashFolder:    withDefaultStr(acct.TrashFolder, raw.Defaults.TrashFolder),
			ArchiveFolder:  withDefaultStr(acct.ArchiveFolder, raw.Defaults.ArchiveFolder),
			Timeout:        timeout,
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

// parseTimeout reads a Go duration such as "10s". Empty means the built-in
// defaults, which is not the same as zero: a zero timeout would mean no
// deadline at all, and a config that asks for one is likelier to be a mistake
// than a request to wait forever.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("timeout %q is not a duration such as \"30s\": %w", s, mail.ErrInvalidConfig)
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout %q must be positive: %w", s, mail.ErrInvalidConfig)
	}
	return d, nil
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
    "draftsFolder": "",
    "trashFolder": "",
    "archiveFolder": "",
    "timeout": ""
  },
  "accounts": [
    {
      "address": "you@example.com",
      "displayName": "Your Name",
      "default": true,
      "imap": { "host": "imap.example.com", "port": 993, "encryption": "tls" },
      "smtp": { "host": "smtp.example.com", "port": 587, "encryption": "starttls" },
      "password": "",
      "passwordFile": "~/.bifrost/pass-you@example.com",
      "authMechanism": "",
      "tokenCommand": [],
      "sentFolder": "",
      "draftsFolder": "",
      "trashFolder": "",
      "archiveFolder": ""
    }
  ]
}
`
}
