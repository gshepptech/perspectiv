// Package mailer sends transactional email through one interface. Resend is
// the production provider; the SMTP mailer delivers to Mailpit in
// development. The provider is the only third-party service the app calls at
// runtime.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Mailer sends one message. An error means the message was not accepted by
// the provider; the caller decides whether to retry.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Message is one email to a single recipient. Text is required; HTML is an
// optional alternative rendering.
type Message struct {
	To, Subject, Text, HTML string
}

// validate refuses a message that would be malformed on the wire: a missing
// or unparsable address, an empty body, or a header value carrying a line
// break (which would let content inject extra headers such as Bcc).
func validate(from string, m Message) error {
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("sender %q: %w", from, err)
	}
	if m.To == "" {
		return errors.New("recipient is required")
	}
	if _, err := mail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("recipient: %w", err)
	}
	if m.Text == "" {
		return errors.New("text body is required")
	}
	for name, v := range map[string]string{"sender": from, "recipient": m.To, "subject": m.Subject} {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("%s contains a line break", name)
		}
	}
	return nil
}
