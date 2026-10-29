package auth

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestAccessTokenRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now()
	iss := tokenIssuer{secret: []byte(testSecret), now: func() time.Time { return now }}
	user, sess := uuid.New(), uuid.New()
	tok, exp, err := iss.issue(user, sess, perspectivv1.Role_ROLE_ADMIN)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if want := now.Add(AccessTokenTTL).Truncate(time.Second); !exp.Equal(want) {
		t.Fatalf("expiry = %v, want %v", exp, want)
	}
	gotUser, gotSess, err := iss.parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gotUser != user || gotSess != sess {
		t.Fatalf("parse = %v %v, want %v %v", gotUser, gotSess, user, sess)
	}
}

func TestAccessTokenRejected(t *testing.T) {
	t.Parallel()
	iss := tokenIssuer{secret: []byte(testSecret), now: time.Now}
	sign := func(method jwt.SigningMethod, key any, claims jwt.Claims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return s
	}
	good := func(mut func(*accessClaims)) *accessClaims {
		c := &accessClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   uuid.NewString(),
				Issuer:    jwtIssuer,
				Audience:  jwt.ClaimStrings{jwtAudience},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			},
			SessionID: uuid.NewString(),
		}
		if mut != nil {
			mut(c)
		}
		return c
	}
	tests := []struct {
		name  string
		token string
	}{
		{name: "garbage", token: "not.a.jwt"},
		{name: "wrong secret", token: sign(jwt.SigningMethodHS256, []byte(strings.Repeat("z", 32)), good(nil))},
		{name: "alg none", token: sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, good(nil))},
		{name: "HS512 is not on the allow-list", token: sign(jwt.SigningMethodHS512, []byte(testSecret), good(nil))},
		{name: "expired", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}))},
		{name: "no expiry", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) { c.ExpiresAt = nil }))},
		{name: "wrong issuer", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) { c.Issuer = "other" }))},
		{name: "wrong audience", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) {
			c.Audience = jwt.ClaimStrings{"other"}
		}))},
		{name: "subject not a uuid", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) { c.Subject = "bob" }))},
		{name: "session not a uuid", token: sign(jwt.SigningMethodHS256, []byte(testSecret), good(func(c *accessClaims) { c.SessionID = "" }))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := iss.parse(tc.token); !errors.Is(err, errBadAccessToken) {
				t.Fatalf("parse = %v, want errBadAccessToken", err)
			}
		})
	}
}

func TestBearerToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{name: "no header", header: ""},
		{name: "bearer", header: "Bearer abc", want: "abc", ok: true},
		{name: "bearer in lower case", header: "bearer abc", want: "abc", ok: true},
		{name: "basic", header: "Basic abc"},
		{name: "bearer with nothing after it", header: "Bearer "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			if tc.header != "" {
				h.Set("Authorization", tc.header)
			}
			got, ok := bearerToken(h)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("bearerToken = %q %v, want %q %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestOpaqueTokenHashing(t *testing.T) {
	t.Parallel()
	a, ha, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("newOpaqueToken: %v", err)
	}
	b, _, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("newOpaqueToken: %v", err)
	}
	if a == b {
		t.Fatal("two tokens are equal")
	}
	if len(a) < 43 {
		t.Fatalf("token %q is shorter than 256 bits of base64", a)
	}
	if string(hashToken(a)) != string(ha) {
		t.Fatal("hashToken does not reproduce the stored hash")
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "valid", cfg: Config{JWTSecret: testSecret, PublicBaseURL: "https://perspectiv.example", SignUpMode: SignUpOpen}},
		{name: "invite-only", cfg: Config{JWTSecret: testSecret, PublicBaseURL: "https://perspectiv.example", SignUpMode: SignUpInvite}},
		{name: "short secret", cfg: Config{JWTSecret: "short", PublicBaseURL: "https://perspectiv.example", SignUpMode: SignUpOpen}, wantErr: true},
		{name: "no base url", cfg: Config{JWTSecret: testSecret, SignUpMode: SignUpOpen}, wantErr: true},
		{name: "relative base url", cfg: Config{JWTSecret: testSecret, PublicBaseURL: "/app", SignUpMode: SignUpOpen}, wantErr: true},
		{name: "no sign-up mode", cfg: Config{JWTSecret: testSecret, PublicBaseURL: "https://perspectiv.example"}, wantErr: true},
		{name: "unknown sign-up mode", cfg: Config{JWTSecret: testSecret, PublicBaseURL: "https://perspectiv.example", SignUpMode: "closed"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.cfg.validate(); (err != nil) != tc.wantErr {
				t.Fatalf("validate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRefreshCookie(t *testing.T) {
	t.Parallel()
	exp := time.Now().Add(time.Hour)
	tests := []struct {
		name       string
		secure     bool
		value      string
		wantMaxAge func(int) bool
	}{
		{name: "a set cookie lives until the token expires", secure: true, value: "tok", wantMaxAge: func(n int) bool { return n > 3500 && n <= 3600 }},
		{name: "a cleared cookie expires now", secure: true, value: "", wantMaxAge: func(n int) bool { return n < 0 }},
		{name: "secure follows the config", secure: false, value: "tok", wantMaxAge: func(n int) bool { return n > 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := Handler{cookieSecure: tc.secure}
			var c *http.Cookie
			if tc.value == "" {
				c = h.clearCookie()
			} else {
				c = h.refreshCookie(tc.value, exp)
			}
			parsed, err := http.ParseSetCookie(c.String())
			if err != nil {
				t.Fatalf("ParseSetCookie(%q): %v", c.String(), err)
			}
			if parsed.Name != RefreshCookieName || parsed.Value != tc.value || parsed.Path != refreshCookiePath ||
				!parsed.HttpOnly || parsed.Secure != tc.secure || parsed.SameSite != http.SameSiteStrictMode ||
				!tc.wantMaxAge(parsed.MaxAge) {
				t.Fatalf("cookie = %q", c.String())
			}
		})
	}
}

func TestCookieValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		headers []string
		want    string
	}{
		{name: "no cookie header", want: ""},
		{name: "only other cookies", headers: []string{"a=1; b=2"}, want: ""},
		{name: "among others", headers: []string{"a=1; " + RefreshCookieName + "=tok; b=2"}, want: "tok"},
		{name: "in a second header line", headers: []string{"a=1", RefreshCookieName + "=tok"}, want: "tok"},
		{name: "a malformed header is skipped", headers: []string{"\x00", RefreshCookieName + "=tok"}, want: "tok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			for _, v := range tc.headers {
				h.Add("Cookie", v)
			}
			if got := refreshCookieValue(h); got != tc.want {
				t.Fatalf("refreshCookieValue = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateRules(t *testing.T) {
	t.Parallel()
	for action, r := range rateRules {
		if r.perUser <= 0 || r.perIP <= 0 || r.windowSecs <= 0 {
			t.Errorf("rule %q = %+v; every limit and window must be positive", action, r)
		}
	}
	for _, action := range []string{
		api.LimitSignup, api.LimitLogin, api.LimitPublish, api.LimitClaim, api.LimitVote, api.LimitReact, api.LimitReport,
	} {
		if _, ok := rateRules[action]; !ok {
			t.Errorf("no rule for the route action %q", action)
		}
	}
}
