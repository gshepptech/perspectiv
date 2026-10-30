package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/notify/dbq"
)

// Unsubscriber signs and checks the account id in an unsubscribe link, so a
// link turns off one account's emails without a sign-in and cannot be
// forged for another account. The signature never expires: a link in an
// old email keeps working, as mail clients and the law expect.
type Unsubscriber struct {
	key []byte
}

// NewUnsubscriber derives the signing key from the server secret. Rotating
// the secret retires every link already sent; the reader can still turn
// emails off in Settings.
func NewUnsubscriber(secret string) Unsubscriber {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("perspectiv/unsubscribe/v1"))
	return Unsubscriber{key: mac.Sum(nil)}
}

func (u Unsubscriber) token(userID uuid.UUID) string {
	mac := hmac.New(sha256.New, u.key)
	mac.Write(userID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (u Unsubscriber) valid(userID uuid.UUID, token string) bool {
	if len(u.key) == 0 {
		return false
	}
	return hmac.Equal([]byte(u.token(userID)), []byte(token))
}

// query is the u and t parameters of both unsubscribe URLs.
func (u Unsubscriber) query(userID uuid.UUID) string {
	return url.Values{"u": {userID.String()}, "t": {u.token(userID)}}.Encode()
}

// Paths of the two unsubscribe URLs: the page a reader opens from the email
// body, and the endpoint a mail client POSTs to for a one-click unsubscribe.
const (
	unsubscribePage     = "/unsubscribe"
	unsubscribeOneClick = "/email/unsubscribe"
)

var errBadUnsubscribe = apiutil.InvalidArg("this unsubscribe link is not valid")

// Unsubscribe turns email off for every notification kind of the account in
// a signed unsubscribe link. In-app notifications and the daily summary
// setting are left as they are; with every email off, no summary is sent.
func (s *Service) Unsubscribe(ctx context.Context, rawUserID, token string) error {
	id, err := uuid.Parse(rawUserID)
	if err != nil || !s.unsub.valid(id, token) {
		return errBadUnsubscribe
	}
	kinds := make([]string, 0, len(allKinds))
	for _, k := range allKinds {
		kinds = append(kinds, kindText(k))
	}
	if err := dbq.New(s.pool).DisableEmail(ctx, dbq.DisableEmailParams{UserID: id, Kinds: kinds}); err != nil {
		return apiutil.Internal(err)
	}
	return nil
}

// OneClickMount is the RFC 8058 one-click endpoint, mounted under /api. A
// mail client POSTs to it with the link's own query; it answers 200 once
// email is off, 400 for a forged link and 405 for anything but POST, so a
// link scanner that GETs it changes nothing.
func (h *Handler) OneClickMount(_ ...connect.HandlerOption) (string, http.Handler) {
	return unsubscribeOneClick, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		if err := h.svc.Unsubscribe(r.Context(), q.Get("u"), q.Get("t")); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errBadUnsubscribe) {
				status = http.StatusBadRequest
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
