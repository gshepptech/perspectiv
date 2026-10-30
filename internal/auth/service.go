// Package auth owns sign up, sign in and the security state behind them:
// passwords, sessions and their rotating refresh tokens, access tokens,
// email verification and password reset links, the login lockout and the
// rate-limit counters. Every counter and revocation lives in PostgreSQL.
//
// The Service is the API's Authenticator and Gate and the Limiter is its
// Limiter; the composition root passes them to api.Interceptors.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
	"perspectiv/internal/user"
)

// Login lockout: LockoutFailures wrong passwords for one account inside the
// trailing LockoutWindow refuse every login to it, the right password
// included, until the oldest of those failures leaves the window. The count
// is read from login_attempts, so it survives a restart. A successful login
// does not reset it, and a login refused by the lockout is not itself
// counted, so the lockout lifts LockoutWindow after the failures stop.
const (
	LockoutFailures = 10
	LockoutWindow   = 15 * time.Minute
)

// email_tokens.purpose values.
const (
	purposeVerifyEmail   = "verify_email"
	purposePasswordReset = "password_reset"
)

// Pages the emailed links open in the web app.
const (
	verifyEmailPage   = "/verify-email"
	resetPasswordPage = "/reset-password"
)

const uniqueViolation = "23505"

// Config is the auth service's settings, read by the composition root.
type Config struct {
	// JWTSecret signs access tokens with HS256; at least MinJWTSecretBytes.
	JWTSecret string
	// PublicBaseURL is the site's origin, the base of every emailed link.
	PublicBaseURL string
	// CookieSecure marks the refresh cookie Secure. Only a plain-HTTP local
	// setup turns it off.
	CookieSecure bool
	// BcryptCost is the password hash cost; 0 selects DefaultBcryptCost.
	BcryptCost int
	// SignUpMode is open or invite; there is no default, so a deployment
	// states which it runs.
	SignUpMode SignUpMode
}

func (c Config) validate() error {
	if len(c.JWTSecret) < MinJWTSecretBytes {
		return fmt.Errorf("auth: JWT secret must be at least %d bytes", MinJWTSecretBytes)
	}
	if c.SignUpMode != SignUpOpen && c.SignUpMode != SignUpInvite {
		return fmt.Errorf("auth: sign-up mode %q must be open or invite", c.SignUpMode)
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("auth: public base URL %q must be an absolute http(s) URL", c.PublicBaseURL)
	}
	return nil
}

// Service is the auth domain's logic over its sqlc queries.
type Service struct {
	pool      *pgxpool.Pool
	q         *dbq.Queries
	enq       *jobs.Enqueuer
	lim       *Limiter
	passwords Passwords
	tokens    tokenIssuer
	cfg       Config
	log       zerolog.Logger
	// dummyHash is verified against when the email has no account, so an
	// unknown email takes as long to refuse as a wrong password.
	dummyHash string
}

// NewService builds the service. All arguments are required.
func NewService(pool *pgxpool.Pool, enq *jobs.Enqueuer, lim *Limiter, cfg Config, log zerolog.Logger) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	pw := NewPasswords(cfg.BcryptCost)
	dummy, err := pw.dummyHash()
	if err != nil {
		return nil, err
	}
	return &Service{
		pool:      pool,
		q:         dbq.New(pool),
		enq:       enq,
		lim:       lim,
		passwords: pw,
		tokens:    tokenIssuer{secret: []byte(cfg.JWTSecret), now: time.Now},
		cfg:       cfg,
		log:       log.With().Str("component", "auth").Logger(),
		dummyHash: dummy,
	}, nil
}

// VerifyPassword checks a password against a stored hash; it lets other
// domains confirm a password without knowing the hash format.
func (s *Service) VerifyPassword(hash, password string) (bool, error) {
	return s.passwords.VerifyPassword(hash, password)
}

// inTx runs fn in a transaction and commits it when fn returns nil.
func (s *Service) inTx(ctx context.Context, fn func(q *dbq.Queries, tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }() // a no-op after Commit
	if err := fn(s.q.WithTx(tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return nil
}

func internal(what string, err error) error {
	return apiutil.Internal(fmt.Errorf("%s: %w", what, err))
}

// link is an emailed link to page carrying token.
func (s *Service) link(page, token string) string {
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + page + "?token=" + url.QueryEscape(token)
}

// SignUpInput is one sign up request.
type SignUpInput struct {
	Email        string
	Password     string
	Username     string
	AgeConfirmed bool
	// AccessCode admits the account while sign-up is invite-only.
	AccessCode string
	// Honeypot is the form field hidden from people; a value means a bot.
	Honeypot string
}

// SignUp creates an unverified account and, in the same transaction, a
// verification token and the job that emails its link. The account can sign
// in at once; content writes wait for verification.
func (s *Service) SignUp(ctx context.Context, in SignUpInput) (uuid.UUID, error) {
	// A filled honeypot is answered like a success so the filler learns
	// nothing; nothing is stored and no email is sent.
	if in.Honeypot != "" {
		s.log.Info().Str("event", "signup_honeypot").Msg("automated sign-up ignored")
		return uuid.New(), nil
	}
	if !in.AgeConfirmed {
		return uuid.Nil, apiutil.InvalidArg("you must confirm you are 18 or over")
	}
	email, err := validateEmail(in.Email)
	if err != nil {
		return uuid.Nil, err
	}
	if err := user.ValidateUsername(in.Username); err != nil {
		return uuid.Nil, err
	}
	if err := ValidatePassword(in.Password, email, in.Username); err != nil {
		return uuid.Nil, err
	}
	hash, err := s.passwords.Hash(in.Password)
	if err != nil {
		return uuid.Nil, apiutil.Internal(err)
	}
	tok, tokHash, err := newOpaqueToken()
	if err != nil {
		return uuid.Nil, apiutil.Internal(err)
	}
	var id uuid.UUID
	err = s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		// The code is checked before the account exists, so a refused
		// sign-up reveals nothing about which emails or usernames are taken.
		joined, err := s.admit(ctx, q, in.AccessCode)
		if err != nil {
			return err
		}
		id, err = q.InsertUser(ctx, dbq.InsertUserParams{Email: email, PasswordHash: hash, Username: in.Username, JoinedWith: joined})
		if err != nil {
			return signUpConflict(err)
		}
		if err := q.InsertEmailToken(ctx, dbq.InsertEmailTokenParams{
			UserID: id, Purpose: purposeVerifyEmail, TokenHash: tokHash, ExpiresAt: time.Now().Add(VerifyTokenTTL),
		}); err != nil {
			return internal("insert verification token", err)
		}
		if err := s.enq.EmailTx(ctx, tx, jobs.EmailArgs{
			To: email, Template: jobs.TemplateVerifyEmail,
			Data: map[string]string{"link": s.link(verifyEmailPage, tok), "username": in.Username},
		}); err != nil {
			return apiutil.Internal(err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.log.Info().Str("event", "signup").Str("sub", id.String()).Msg("account created")
	return id, nil
}

// signUpConflict maps a users insert error: a case-insensitive email or
// username collision is AlreadyExists, anything else is internal.
func signUpConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		switch pgErr.ConstraintName {
		case "users_email_lower_key":
			return apiutil.AlreadyExists("an account with that email already exists")
		case "users_username_lower_key":
			return apiutil.AlreadyExists("that username is taken")
		}
	}
	return internal("insert user", err)
}

// SessionUser is the signed-in user as the web app's session shows it.
type SessionUser struct {
	ID            uuid.UUID
	Username      string
	Role          perspectivv1.Role
	EmailVerified bool
}

// Tokens is a signed-in session's credentials: the access token for the
// response body and the refresh token for the cookie.
type Tokens struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             SessionUser
}

var (
	errBadCredentials = apiutil.Unauthenticated("email or password is incorrect")
	errLockedOut      = apiutil.ResourceExhausted("too many failed sign-in attempts; try again in 15 minutes")
)

// Login checks the password under the account lockout and starts a session,
// labelled with the browser and system userAgent names.
// Every attempt on an existing account is recorded in login_attempts.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Tokens, error) {
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = s.passwords.VerifyPassword(s.dummyHash, password)
		s.log.Warn().Str("event", "login_failed").Str("reason", "unknown_email").Msg("login refused")
		return Tokens{}, errBadCredentials
	}
	if err != nil {
		return Tokens{}, internal("get user", err)
	}
	role, err := roleFromDB(u.Role)
	if err != nil {
		return Tokens{}, apiutil.Internal(err)
	}
	var out Tokens
	var refused error
	err = s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		if err := q.LockLoginAttempts(ctx, u.ID); err != nil {
			return internal("lock login attempts", err)
		}
		failures, err := q.CountRecentFailedLogins(ctx, dbq.CountRecentFailedLoginsParams{
			UserID: u.ID, WindowMinutes: int32(LockoutWindow / time.Minute),
		})
		if err != nil {
			return internal("count failed logins", err)
		}
		if failures >= LockoutFailures {
			refused = errLockedOut
			return nil
		}
		ok, err := s.passwords.VerifyPassword(u.PasswordHash, password)
		if err != nil {
			return apiutil.Internal(err)
		}
		if err := q.InsertLoginAttempt(ctx, dbq.InsertLoginAttemptParams{UserID: u.ID, Succeeded: ok}); err != nil {
			return internal("record login attempt", err)
		}
		if !ok {
			refused = errBadCredentials
			return nil
		}
		out, err = s.startSession(ctx, q, deviceLabel(userAgent), SessionUser{
			ID: u.ID, Username: u.Username, Role: role, EmailVerified: u.EmailVerifiedAt != nil,
		})
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	switch {
	case errors.Is(refused, errLockedOut):
		s.log.Warn().Str("event", "login_locked").Str("sub", u.ID.String()).Msg("login refused by lockout")
		return Tokens{}, refused
	case errors.Is(refused, errBadCredentials):
		s.log.Warn().Str("event", "login_failed").Str("reason", "wrong_password").Str("sub", u.ID.String()).Msg("login refused")
		return Tokens{}, refused
	}
	s.log.Info().Str("event", "login_succeeded").Str("sub", u.ID.String()).Msg("signed in")
	return out, nil
}

// startSession creates a session with its first refresh token and signs an
// access token for it.
func (s *Service) startSession(ctx context.Context, q *dbq.Queries, device string, u SessionUser) (Tokens, error) {
	now := time.Now()
	sid, err := q.InsertSession(ctx, dbq.InsertSessionParams{UserID: u.ID, ExpiresAt: now.Add(SessionMaxAge), Device: device})
	if err != nil {
		return Tokens{}, internal("insert session", err)
	}
	return s.rotate(ctx, q, u, sid, nil, now.Add(SessionMaxAge))
}

// rotate issues a refresh token for a session, chained to parent when the
// session already had one, and an access token. The refresh token expires
// RefreshTokenTTL from now, or with the session if that is sooner.
func (s *Service) rotate(ctx context.Context, q *dbq.Queries, u SessionUser, sid uuid.UUID, parent *uuid.UUID,
	sessionExpires time.Time,
) (Tokens, error) {
	tok, tokHash, err := newOpaqueToken()
	if err != nil {
		return Tokens{}, apiutil.Internal(err)
	}
	exp := time.Now().Add(RefreshTokenTTL)
	if sessionExpires.Before(exp) {
		exp = sessionExpires
	}
	if err := q.InsertRefreshToken(ctx, dbq.InsertRefreshTokenParams{
		SessionID: sid, ParentID: parent, TokenHash: tokHash, ExpiresAt: exp,
	}); err != nil {
		return Tokens{}, internal("insert refresh token", err)
	}
	access, accessExp, err := s.tokens.issue(u.ID, sid, u.Role)
	if err != nil {
		return Tokens{}, apiutil.Internal(err)
	}
	return Tokens{
		AccessToken: access, AccessExpiresAt: accessExp,
		RefreshToken: tok, RefreshExpiresAt: exp,
		User: u,
	}, nil
}
