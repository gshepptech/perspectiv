package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// Token lifetimes.
//
//   - An access token lives AccessTokenTTL. The browser holds it in memory and
//     refreshes it shortly before expiry.
//   - A refresh token lives RefreshTokenTTL from its issue, and every refresh
//     issues a new one, so a session stays signed in while it is used at least
//     once a RefreshTokenTTL.
//   - A session ends SessionMaxAge after sign in however often it refreshes;
//     the user then signs in again.
//   - An email verification link lives VerifyTokenTTL, long enough to survive a
//     weekend away from the inbox.
//   - A password reset link lives ResetTokenTTL: it grants the account, so it
//     is kept short.
const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour
	SessionMaxAge   = 90 * 24 * time.Hour
	VerifyTokenTTL  = 48 * time.Hour
	ResetTokenTTL   = time.Hour
)

// MinJWTSecretBytes is the shortest HS256 signing secret accepted: the
// algorithm's full 256-bit key.
const MinJWTSecretBytes = 32

const (
	jwtIssuer   = "perspectiv"
	jwtAudience = "perspectiv-api"
	// opaqueTokenBytes is the entropy of refresh and email tokens.
	opaqueTokenBytes = 32
)

// errBadAccessToken is any access token that does not verify: malformed,
// wrongly signed, on an algorithm outside the allow-list, expired, or for
// another issuer or audience.
var errBadAccessToken = errors.New("invalid access token")

// accessClaims are the access JWT's claims. sub is the user id and sid the
// session id. roles records the role at issue for clients that read it; the
// server never trusts it and reads the role from Postgres on every call.
type accessClaims struct {
	jwt.RegisteredClaims
	SessionID string   `json:"sid"`
	Roles     []string `json:"roles,omitempty"`
}

// tokenIssuer signs and verifies HS256 access tokens.
type tokenIssuer struct {
	secret []byte
	now    func() time.Time
}

// issue returns a signed access token for a session and its expiry.
func (t tokenIssuer) issue(userID, sessionID uuid.UUID, role perspectivv1.Role) (string, time.Time, error) {
	now := t.now()
	exp := jwt.NewNumericDate(now.Add(AccessTokenTTL))
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: exp,
			ID:        uuid.NewString(),
		},
		SessionID: sessionID.String(),
		Roles:     []string{role.String()},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return s, exp.Time, nil
}

// parse verifies an access token and returns its user and session ids. Every
// failure is errBadAccessToken.
func (t tokenIssuer) parse(s string) (userID, sessionID uuid.UUID, err error) {
	var c accessClaims
	_, err = jwt.ParseWithClaims(s, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithAudience(jwtAudience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %w", errBadAccessToken, err)
	}
	userID, uerr := uuid.Parse(c.Subject)
	sessionID, serr := uuid.Parse(c.SessionID)
	if uerr != nil || serr != nil || userID == uuid.Nil || sessionID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: bad subject or session", errBadAccessToken)
	}
	return userID, sessionID, nil
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header.
func bearerToken(h http.Header) (string, bool) {
	scheme, tok, found := strings.Cut(h.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}

// newOpaqueToken returns a random URL-safe token and the hash stored in its
// place. Only the hash reaches Postgres, so a read of the table cannot be
// replayed as a refresh cookie or an email link.
func newOpaqueToken() (string, []byte, error) {
	b := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, hashToken(tok), nil
}

// hashToken is the stored form of an opaque token. The token carries 256
// random bits, so an unsalted SHA-256 is enough and lets the token be found
// by its hash.
func hashToken(tok string) []byte {
	sum := sha256.Sum256([]byte(tok))
	return sum[:]
}
