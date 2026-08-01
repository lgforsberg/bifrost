package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lgforsberg/bifrost/mail"
)

func writeConfig(t *testing.T, json string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(json), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_MinimalValid(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "secret"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("got %d accounts", len(cfg.Accounts))
	}
	a := cfg.Accounts[0]
	if a.Address != "a@example.com" {
		t.Errorf("Address = %q", a.Address)
	}
	if a.IMAPPort != 993 {
		t.Errorf("IMAPPort = %d, want 993", a.IMAPPort)
	}
	if a.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want 587", a.SMTPPort)
	}
	if a.IMAPEncryption != "tls" {
		t.Errorf("IMAPEncryption = %q, want tls", a.IMAPEncryption)
	}
	if a.SMTPEncryption != "starttls" {
		t.Errorf("SMTPEncryption = %q, want starttls", a.SMTPEncryption)
	}
}

func TestLoad_DefaultsOmittedMeansTrue(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "s"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Defaults.QuoteReplies {
		t.Error("QuoteReplies should default to true when omitted")
	}
	if !cfg.Defaults.SaveToSent {
		t.Error("SaveToSent should default to true when omitted")
	}
	if cfg.Defaults.PeekOnRead {
		t.Error("PeekOnRead should default to false")
	}
}

func TestLoad_DefaultsExplicitFalse(t *testing.T) {
	path := writeConfig(t, `{
		"defaults": {"quoteReplies": false, "saveToSent": false},
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "s"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Defaults.QuoteReplies {
		t.Error("QuoteReplies should be false when explicitly set")
	}
	if cfg.Defaults.SaveToSent {
		t.Error("SaveToSent should be false when explicitly set")
	}
}

// These were parsed and documented but never reached the account, so
// configuring them did nothing at all.
func TestLoad_SpecialFolderOverridesReachTheAccount(t *testing.T) {
	path := writeConfig(t, `{
		"defaults": {
			"sentFolder": "Skickat",
			"draftsFolder": "Utkast",
			"trashFolder": "Papperskorg",
			"archiveFolder": "Arkiv"
		},
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "secret"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	a := cfg.Accounts[0]
	for attr, want := range map[string]string{
		"\\Sent":    "Skickat",
		"\\Drafts":  "Utkast",
		"\\Trash":   "Papperskorg",
		"\\Archive": "Arkiv",
	} {
		if got := a.SpecialFolderOverride(attr); got != want {
			t.Errorf("override for %s = %q, want %q", attr, got, want)
		}
	}
}

// Two accounts on different providers rarely agree on folder names, so an
// account's own override has to beat the shared default.
func TestLoad_AccountOverridesBeatDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"defaults": {
			"archiveFolder": "Arkiv",
			"trashFolder": "Papperskorg"
		},
		"accounts": [
			{
				"address": "se@example.com",
				"imap": {"host": "imap.example.com"},
				"smtp": {"host": "smtp.example.com"},
				"password": "secret"
			},
			{
				"address": "us@example.net",
				"archiveFolder": "Archive",
				"imap": {"host": "imap.example.net"},
				"smtp": {"host": "smtp.example.net"},
				"password": "secret"
			}
		]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Accounts[0].SpecialFolderOverride("\\Archive"); got != "Arkiv" {
		t.Errorf("first account archive = %q, want the default Arkiv", got)
	}
	if got := cfg.Accounts[1].SpecialFolderOverride("\\Archive"); got != "Archive" {
		t.Errorf("second account archive = %q, want its own Archive", got)
	}
	// An account that overrides one folder still inherits the rest.
	if got := cfg.Accounts[1].SpecialFolderOverride("\\Trash"); got != "Papperskorg" {
		t.Errorf("second account trash = %q, want the inherited Papperskorg", got)
	}
}

func TestLoad_NoOverridesLeavesResolutionToTheServer(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "secret"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	a := cfg.Accounts[0]
	for _, attr := range []string{"\\Sent", "\\Drafts", "\\Trash", "\\Archive", "\\Junk"} {
		if got := a.SpecialFolderOverride(attr); got != "" {
			t.Errorf("override for %s = %q, want none", attr, got)
		}
	}
}

func TestLoad_MissingAddress(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"password": "s"
		}]
	}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestLoad_MissingPassword(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"}
		}]
	}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing password")
	}
	if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestLoad_NoAccounts(t *testing.T) {
	path := writeConfig(t, `{"accounts": []}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty accounts")
	}
	if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/tmp/nonexistent-bifrost-config-12345.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	path := writeConfig(t, `{not json}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_PasswordFile(t *testing.T) {
	dir := t.TempDir()
	passPath := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(passPath, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com"},
			"smtp": {"host": "smtp.example.com"},
			"passwordFile": "`+passPath+`"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Accounts[0].Password != "file-secret" {
		t.Errorf("Password = %q, want %q", cfg.Accounts[0].Password, "file-secret")
	}
}

func TestLoad_CustomPorts(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "a@example.com",
			"imap": {"host": "imap.example.com", "port": 143, "encryption": "starttls"},
			"smtp": {"host": "smtp.example.com", "port": 465, "encryption": "tls"},
			"password": "s"
		}]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := cfg.Accounts[0]
	if a.IMAPPort != 143 {
		t.Errorf("IMAPPort = %d", a.IMAPPort)
	}
	if a.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d", a.SMTPPort)
	}
	if a.IMAPEncryption != "starttls" {
		t.Errorf("IMAPEncryption = %q", a.IMAPEncryption)
	}
	if a.SMTPEncryption != "tls" {
		t.Errorf("SMTPEncryption = %q", a.SMTPEncryption)
	}
}

// --- Account resolution ---

func twoAccountConfig(t *testing.T) *Config {
	t.Helper()
	path := writeConfig(t, `{
		"accounts": [
			{
				"address": "alice@example.com",
				"displayName": "Alice",
				"default": true,
				"imap": {"host": "imap.example.com"},
				"smtp": {"host": "smtp.example.com"},
				"password": "s"
			},
			{
				"address": "bob@work.com",
				"displayName": "Bob",
				"imap": {"host": "imap.work.com"},
				"smtp": {"host": "smtp.work.com"},
				"password": "s"
			}
		]
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestAccountByAddress_Exact(t *testing.T) {
	cfg := twoAccountConfig(t)
	acct, err := AccountByAddress(cfg, "bob@work.com")
	if err != nil {
		t.Fatalf("AccountByAddress: %v", err)
	}
	if acct.Address != "bob@work.com" {
		t.Errorf("got %q", acct.Address)
	}
}

func TestAccountByAddress_CaseInsensitive(t *testing.T) {
	cfg := twoAccountConfig(t)
	acct, err := AccountByAddress(cfg, "Alice@Example.COM")
	if err != nil {
		t.Fatalf("AccountByAddress: %v", err)
	}
	if acct.Address != "alice@example.com" {
		t.Errorf("got %q", acct.Address)
	}
}

func TestAccountByAddress_Partial(t *testing.T) {
	cfg := twoAccountConfig(t)
	acct, err := AccountByAddress(cfg, "bob")
	if err != nil {
		t.Fatalf("AccountByAddress: %v", err)
	}
	if acct.Address != "bob@work.com" {
		t.Errorf("got %q", acct.Address)
	}
}

func TestAccountByAddress_Ambiguous(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [
			{
				"address": "test@alpha.com",
				"imap": {"host": "h"}, "smtp": {"host": "h"}, "password": "s"
			},
			{
				"address": "test@beta.com",
				"imap": {"host": "h"}, "smtp": {"host": "h"}, "password": "s"
			}
		]
	}`)
	cfg, _ := Load(path)
	_, err := AccountByAddress(cfg, "test")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestAccountByAddress_NotFound(t *testing.T) {
	cfg := twoAccountConfig(t)
	_, err := AccountByAddress(cfg, "nobody@nowhere.com")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, mail.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestAccountByAddress_PlusTagNormalized(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [{
			"address": "user@example.com",
			"imap": {"host": "h"}, "smtp": {"host": "h"}, "password": "s"
		}]
	}`)
	cfg, _ := Load(path)
	acct, err := AccountByAddress(cfg, "user+tag@example.com")
	if err != nil {
		t.Fatalf("AccountByAddress: %v", err)
	}
	if acct.Address != "user@example.com" {
		t.Errorf("got %q", acct.Address)
	}
}

// --- Default account ---

func TestDefaultAccount_MarkedDefault(t *testing.T) {
	cfg := twoAccountConfig(t)
	acct, err := DefaultAccount(cfg)
	if err != nil {
		t.Fatalf("DefaultAccount: %v", err)
	}
	if acct.Address != "alice@example.com" {
		t.Errorf("got %q, want alice (marked default)", acct.Address)
	}
}

func TestDefaultAccount_FallsBackToFirst(t *testing.T) {
	path := writeConfig(t, `{
		"accounts": [
			{"address": "first@example.com", "imap": {"host": "h"}, "smtp": {"host": "h"}, "password": "s"},
			{"address": "second@example.com", "imap": {"host": "h"}, "smtp": {"host": "h"}, "password": "s"}
		]
	}`)
	cfg, _ := Load(path)
	acct, err := DefaultAccount(cfg)
	if err != nil {
		t.Fatalf("DefaultAccount: %v", err)
	}
	if acct.Address != "first@example.com" {
		t.Errorf("got %q, want first account as fallback", acct.Address)
	}
}

func TestIsDefaultAccount(t *testing.T) {
	cfg := twoAccountConfig(t)
	if !IsDefaultAccount(cfg, 0) {
		t.Error("index 0 should be default")
	}
	if IsDefaultAccount(cfg, 1) {
		t.Error("index 1 should not be default")
	}
}
