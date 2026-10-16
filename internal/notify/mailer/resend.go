package mailer

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v3"
)

type resendMailer struct {
	client *resend.Client
	from   string
}

// NewResend returns a Mailer sending through the Resend API with apiKey, from
// the from address.
func NewResend(apiKey, from string) Mailer {
	return NewResendClient(resend.NewClient(apiKey), from)
}

// NewResendClient returns a Mailer sending through a caller-built Resend
// client, for a custom HTTP client or API base URL.
func NewResendClient(c *resend.Client, from string) Mailer {
	return &resendMailer{client: c, from: from}
}

func (r *resendMailer) Send(ctx context.Context, m Message) error {
	if err := validate(r.from, m); err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	_, err := r.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    r.from,
		To:      []string{m.To},
		Subject: m.Subject,
		Text:    m.Text,
		Html:    m.HTML,
	})
	if err != nil {
		return fmt.Errorf("resend: send: %w", err)
	}
	return nil
}
