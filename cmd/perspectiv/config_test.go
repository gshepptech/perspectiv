package main

import (
	"strings"
	"testing"
	"time"

	"perspectiv/pkg/crypto"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// with returns base with the given keys overridden; an empty value unsets.
func with(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// testKey is an age key generated for this test process; no key is tracked.
var testKey = func() string {
	k, err := crypto.GenerateKey()
	if err != nil {
		panic(err)
	}
	return k
}()

// validEnv is a complete configuration for ROLE=all.
func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":    "postgres://perspectiv@localhost:5432/db",
		"PUBLIC_BASE_URL": "https://perspectiv.example",
		"JWT_SECRET":      strings.Repeat("s", 32),
		"ENCRYPTION_KEYS": testKey,
		"MAIL_PROVIDER":   "smtp",
		"MAIL_FROM":       "Perspectiv <no-reply@perspectiv.example>",
		"AVATAR_DIR":      "/var/lib/perspectiv/avatars",
	}
}

// GI-005, FR-059: ROLE selects api, worker or all, defaulting to all, and the
// documented defaults apply when a setting is absent.
func TestLoadConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := loadConfig(env(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Role != roleAll || !cfg.runsAPI() || !cfg.runsWorkers() {
		t.Errorf("role = %q (api %v, workers %v), want all running both", cfg.Role, cfg.runsAPI(), cfg.runsWorkers())
	}
	if cfg.HTTPAddr != ":8080" || cfg.SMTPAddr != "localhost:1025" || !cfg.CookieSecure || cfg.MigrateOnStart {
		t.Errorf("defaults: addr %q smtp %q cookieSecure %v migrateOnStart %v", cfg.HTTPAddr, cfg.SMTPAddr, cfg.CookieSecure, cfg.MigrateOnStart)
	}
	if cfg.RankInterval != 60*time.Second || cfg.CanonMinMembers != 4 || cfg.ExpiryWindow != 168*time.Hour || cfg.DBMaxConns != 25 {
		t.Errorf("defaults: rank %v canon %d expiry %v pool %d", cfg.RankInterval, cfg.CanonMinMembers, cfg.ExpiryWindow, cfg.DBMaxConns)
	}
	// Unset, sign-up fails closed: invite-only.
	if cfg.SignUpMode != "invite" {
		t.Errorf("default sign-up mode = %q, want invite", cfg.SignUpMode)
	}
}

func TestLoadConfigRoles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role         string
		api, workers bool
	}{
		{"api", true, false},
		{"worker", false, true},
		{"all", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			cfg, err := loadConfig(env(with(validEnv(), "ROLE", tc.role)))
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.runsAPI() != tc.api || cfg.runsWorkers() != tc.workers {
				t.Errorf("api %v workers %v, want %v %v", cfg.runsAPI(), cfg.runsWorkers(), tc.api, tc.workers)
			}
		})
	}
}

// Each role requires only what it runs: the worker never stores avatars and
// the API never sends mail. Both need JWT_SECRET: the API signs tokens with
// it, and the worker signs unsubscribe links with a key derived from it.
func TestLoadConfigPerRoleRequirements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"unknown role", with(validEnv(), "ROLE", "both"), "ROLE"},
		{"no database", with(validEnv(), "DATABASE_URL", ""), "DATABASE_URL"},
		{"api short JWT secret", with(validEnv(), "ROLE", "api", "JWT_SECRET", "short"), "JWT_SECRET"},
		{"api no avatar dir", with(validEnv(), "ROLE", "api", "AVATAR_DIR", ""), "AVATAR_DIR"},
		{"api no encryption keys", with(validEnv(), "ROLE", "api", "ENCRYPTION_KEYS", ""), "ENCRYPTION_KEYS"},
		{"api encryption key that is not an age key", with(validEnv(), "ROLE", "api", "ENCRYPTION_KEYS", "hunter2"), "ENCRYPTION_KEYS"},
		{"all no encryption keys", with(validEnv(), "ENCRYPTION_KEYS", ""), "ENCRYPTION_KEYS"},
		{"two encryption keys, the current one first", with(validEnv(), "ENCRYPTION_KEYS", testKey+"\n"+testKey), ""},
		{"api relative base URL", with(validEnv(), "ROLE", "api", "PUBLIC_BASE_URL", "perspectiv.example"), "PUBLIC_BASE_URL"},
		{"worker no mail provider", with(validEnv(), "ROLE", "worker", "MAIL_PROVIDER", ""), "MAIL_PROVIDER"},
		{"worker bad mail provider", with(validEnv(), "ROLE", "worker", "MAIL_PROVIDER", "sendmail"), "MAIL_PROVIDER"},
		{"worker resend without key", with(validEnv(), "ROLE", "worker", "MAIL_PROVIDER", "resend"), "RESEND_API_KEY"},
		{"worker no mail from", with(validEnv(), "ROLE", "worker", "MAIL_FROM", ""), "MAIL_FROM"},
		{"all no mail provider", with(validEnv(), "MAIL_PROVIDER", ""), "MAIL_PROVIDER"},
		{"bad rank interval", with(validEnv(), "RANK_INTERVAL", "soon"), "RANK_INTERVAL"},
		{"zero rank interval", with(validEnv(), "RANK_INTERVAL", "0s"), "RANK_INTERVAL"},
		{"bad canon minimum", with(validEnv(), "CANON_MIN_MEMBERS", "0"), "CANON_MIN_MEMBERS"},
		{"bad pool size", with(validEnv(), "DB_MAX_CONNS", "none"), "DB_MAX_CONNS"},
		{"zero pool size", with(validEnv(), "DB_MAX_CONNS", "0"), "DB_MAX_CONNS"},
		{"pool size", with(validEnv(), "DB_MAX_CONNS", "40"), ""},
		{"unknown sign-up mode", with(validEnv(), "SIGNUP_MODE", "closed"), "SIGNUP_MODE"},
		{"open sign-up", with(validEnv(), "SIGNUP_MODE", "open"), ""},
		{"bad expiry window", with(validEnv(), "EXPIRY_WINDOW", "-1h"), "EXPIRY_WINDOW"},
		{"bad cookie flag", with(validEnv(), "COOKIE_SECURE", "maybe"), "COOKIE_SECURE"},
		{"bad migrate flag", with(validEnv(), "MIGRATE_ON_START", "yes please"), "MIGRATE_ON_START"},
		{"worker without API settings", with(validEnv(), "ROLE", "worker", "AVATAR_DIR", "", "ENCRYPTION_KEYS", ""), ""},
		// Workers sign unsubscribe links the API checks, with a key from it.
		{"worker short JWT secret", with(validEnv(), "ROLE", "worker", "JWT_SECRET", "short"), "JWT_SECRET"},
		{"api without mail settings", with(validEnv(), "ROLE", "api", "MAIL_PROVIDER", "", "MAIL_FROM", ""), ""},
		{"worker resend with key", with(validEnv(), "ROLE", "worker", "MAIL_PROVIDER", "resend", "RESEND_API_KEY", "re_x"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadConfig(env(tc.env))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("loadConfig: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("loadConfig error = %v, want one naming %s", err, tc.wantErr)
			}
		})
	}
}

// A configuration error never echoes a secret's value.
func TestLoadConfigErrorHidesSecrets(t *testing.T) {
	t.Parallel()
	secret := "tooshort-but-secret"
	badKey := "AGE-SECRET-KEY-1NOTAREALKEYBUTSECRETANYWAY"
	_, err := loadConfig(env(with(validEnv(), "JWT_SECRET", secret, "RESEND_API_KEY", "re_hidden", "MAIL_PROVIDER", "nope",
		"ENCRYPTION_KEYS", badKey)))
	if err == nil {
		t.Fatal("loadConfig accepted a short secret")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "re_hidden") || strings.Contains(err.Error(), "NOTAREALKEY") {
		t.Fatalf("error leaks a secret: %v", err)
	}
}
