package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		"# comment",
		"",
		"DOTENV_PLAIN=value",
		"export DOTENV_EXPORTED=exported",
		`DOTENV_QUOTED="quoted # not a comment"`,
		"DOTENV_INLINE=value   # inline comment",
		"DOTENV_EMPTY=             # only a comment",
		"DOTENV_SET=from-file",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTENV_SET", "from-environment")
	for _, key := range []string{"DOTENV_PLAIN", "DOTENV_EXPORTED", "DOTENV_QUOTED", "DOTENV_INLINE", "DOTENV_EMPTY"} {
		t.Setenv(key, "") // Registers the cleanup; unset so the file sets it
		os.Unsetenv(key)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DOTENV_PLAIN":    "value",
		"DOTENV_EXPORTED": "exported",
		"DOTENV_QUOTED":   "quoted # not a comment",
		"DOTENV_INLINE":   "value",
		"DOTENV_EMPTY":    "",
		"DOTENV_SET":      "from-environment", // The environment wins
	}
	for key, value := range want {
		if got := os.Getenv(key); got != value {
			t.Errorf("%s = %q; want %q", key, got, value)
		}
	}
}

func TestLoadReportsEveryMissingVariable(t *testing.T) {
	for _, key := range []string{"API_PORT", "SHORT_LINK_URL", "TAGO_ACCOUNT_TOKEN", "TELEGRAM_BOT_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("APP_ENV", "production")
	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded without configuration")
	}
	for _, key := range []string{"API_PORT", "SHORT_LINK_URL", "TAGO_ACCOUNT_TOKEN", "TELEGRAM_BOT_TOKEN"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestCSMSURLs(t *testing.T) {
	r := &reader{}
	t.Setenv("CSMS_URLS", " http://csms-1:8080/ , ,http://csms-2:8080")
	if got := r.optionalURLList("CSMS_URLS"); strings.Join(got, "|") != "http://csms-1:8080|http://csms-2:8080" || r.errs != nil {
		t.Fatalf("got %q, errors %v", got, r.errs)
	}

	t.Setenv("CSMS_URLS", "")
	if got := r.optionalURLList("CSMS_URLS"); got != nil || r.errs != nil {
		t.Fatalf("unset: got %q, errors %v", got, r.errs)
	}

	t.Setenv("CSMS_URLS", "csms-1:8080,ftp://csms")
	if got := r.optionalURLList("CSMS_URLS"); got != nil || len(r.errs) != 2 {
		t.Fatalf("invalid: got %q, errors %v", got, r.errs)
	}
}
