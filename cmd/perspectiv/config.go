package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"perspectiv/internal/auth"
)

// Roles the ROLE setting selects.
const (
	roleAPI    = "api"
	roleWorker = "worker"
	roleAll    = "all"
)

// Mail providers MAIL_PROVIDER selects.
const (
	mailSMTP   = "smtp"
	mailResend = "resend"
)

// defaultDBMaxConns sizes each process's pool. The API and the worker
// together stay well under PostgreSQL's 100 connections at one replica each.
const defaultDBMaxConns = 25

// config is the server's settings. Only this package reads the environment.
type config struct {
	Role        string
	DatabaseURL string
	// DBMaxConns caps this process's pool. pgx's own default is one per
	// CPU (at least four), which a load test filled at 20 readers.
	DBMaxConns      int
	HTTPAddr        string
	PublicBaseURL   string
	JWTSecret       string
	CookieSecure    bool
	MailProvider    string
	SMTPAddr        string
	ResendAPIKey    string
	MailFrom        string
	RankInterval    time.Duration
	CanonMinMembers int
	ExpiryWindow    time.Duration
	AvatarDir       string
	MigrateOnStart  bool
	LogLevel        string
	OTLPEndpoint    string
	// SignUpMode is open or invite (the beta). It defaults to invite, so a
	// deployment that leaves it unset admits nobody without a code.
	SignUpMode string
}

// runsAPI reports whether this process serves the Connect API and the SPA.
func (c config) runsAPI() bool { return c.Role == roleAPI || c.Role == roleAll }

// runsWorkers reports whether this process works River jobs.
func (c config) runsWorkers() bool { return c.Role == roleWorker || c.Role == roleAll }

// loadConfig reads and validates the serve settings. Each role is held only
// to what it runs: JWT_SECRET and AVATAR_DIR are required where the API runs,
// MAIL_PROVIDER and MAIL_FROM where the workers run (the email worker sends
// mail; the API only enqueues it), PUBLIC_BASE_URL for both (auth and email
// links). MAIL_PROVIDER has no default: a worker that silently sent
// production mail to localhost would fail every send. Every problem is
// reported at once, and no message carries a secret's value.
func loadConfig(getenv func(string) string) (config, error) {
	var errs []error
	c := config{
		Role:          orDefault(getenv("ROLE"), roleAll),
		DatabaseURL:   getenv("DATABASE_URL"),
		HTTPAddr:      orDefault(getenv("HTTP_ADDR"), ":8080"),
		PublicBaseURL: getenv("PUBLIC_BASE_URL"),
		JWTSecret:     getenv("JWT_SECRET"),
		MailProvider:  getenv("MAIL_PROVIDER"),
		SMTPAddr:      orDefault(getenv("SMTP_ADDR"), "localhost:1025"),
		ResendAPIKey:  getenv("RESEND_API_KEY"),
		MailFrom:      getenv("MAIL_FROM"),
		AvatarDir:     getenv("AVATAR_DIR"),
		LogLevel:      getenv("LOG_LEVEL"),
		OTLPEndpoint:  getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		SignUpMode:    orDefault(getenv("SIGNUP_MODE"), string(auth.SignUpInvite)),
	}
	c.CookieSecure = parseBool(getenv, "COOKIE_SECURE", true, &errs)
	c.MigrateOnStart = parseBool(getenv, "MIGRATE_ON_START", false, &errs)
	c.RankInterval = parseDuration(getenv, "RANK_INTERVAL", 60*time.Second, &errs)
	c.ExpiryWindow = parseDuration(getenv, "EXPIRY_WINDOW", 168*time.Hour, &errs)
	c.CanonMinMembers = parsePositiveInt(getenv, "CANON_MIN_MEMBERS", 4, &errs)
	c.DBMaxConns = parsePositiveInt(getenv, "DB_MAX_CONNS", defaultDBMaxConns, &errs)

	switch c.Role {
	case roleAPI, roleWorker, roleAll:
	default:
		errs = append(errs, fmt.Errorf("ROLE %q must be api, worker or all", c.Role))
	}
	if c.SignUpMode != string(auth.SignUpOpen) && c.SignUpMode != string(auth.SignUpInvite) {
		errs = append(errs, fmt.Errorf("SIGNUP_MODE %q must be open or invite", c.SignUpMode))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if u, err := url.Parse(c.PublicBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("PUBLIC_BASE_URL must be an absolute http(s) URL"))
	}
	// The API signs tokens with it and the workers sign unsubscribe links
	// with a key derived from it, which the API checks: both need the same.
	if len(c.JWTSecret) < auth.MinJWTSecretBytes {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes", auth.MinJWTSecretBytes))
	}
	if c.runsAPI() {
		errs = append(errs, c.validateAPI()...)
	}
	if c.runsWorkers() {
		errs = append(errs, c.validateWorkers()...)
	}
	return c, errors.Join(errs...)
}

// validateAPI checks what only the API needs.
func (c config) validateAPI() []error {
	var errs []error
	if c.AvatarDir == "" {
		errs = append(errs, errors.New("AVATAR_DIR is required where the API runs"))
	}
	return errs
}

// validateWorkers checks what only the workers need.
func (c config) validateWorkers() []error {
	var errs []error
	switch c.MailProvider {
	case mailSMTP:
	case mailResend:
		if c.ResendAPIKey == "" {
			errs = append(errs, errors.New("RESEND_API_KEY is required when MAIL_PROVIDER is resend"))
		}
	default:
		errs = append(errs, fmt.Errorf("MAIL_PROVIDER %q must be smtp or resend where the workers run", c.MailProvider))
	}
	if c.MailFrom == "" {
		errs = append(errs, errors.New("MAIL_FROM is required where the workers run"))
	}
	return errs
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseBool(getenv func(string) string, key string, def bool, errs *[]error) bool {
	v := getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q is not a boolean", key, v))
		return def
	}
	return b
}

func parseDuration(getenv func(string) string, key string, def time.Duration, errs *[]error) time.Duration {
	v := getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s %q must be a positive duration such as 60s", key, v))
		return def
	}
	return d
}

func parsePositiveInt(getenv func(string) string, key string, def int, errs *[]error) int {
	v := getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		*errs = append(*errs, fmt.Errorf("%s %q must be a positive integer", key, v))
		return def
	}
	return n
}
