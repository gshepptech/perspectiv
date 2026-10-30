package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// smtpTimeout bounds one delivery when the caller's context has no deadline.
const smtpTimeout = 30 * time.Second

type smtpMailer struct {
	addr string
	from string
}

// NewSMTP returns a Mailer delivering through the SMTP server at addr
// (host:port) without authentication. It exists for development, where addr
// is Mailpit; it is not meant for a public relay.
func NewSMTP(addr, from string) Mailer {
	return &smtpMailer{addr: addr, from: from}
}

func (s *smtpMailer) Send(ctx context.Context, m Message) error {
	raw, err := buildMIME(s.from, m)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, smtpTimeout)
		defer cancel()
	}
	if err := s.deliver(ctx, m.To, raw); err != nil {
		return fmt.Errorf("smtp: send to %s: %w", s.addr, err)
	}
	return nil
}

func (s *smtpMailer) deliver(ctx context.Context, to string, raw []byte) error {
	conn, err := new(net.Dialer).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	// The deadline makes every SMTP exchange below honour the context.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		_ = conn.Close()
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()

	from, _ := mail.ParseAddress(s.from)
	rcpt, _ := mail.ParseAddress(to)
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMIME renders m as an RFC 5322 message: text/plain alone, or
// multipart/alternative with text/plain then text/html, both
// quoted-printable.
func buildMIME(from string, m Message) ([]byte, error) {
	if err := validate(from, m); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	hdr("From", from)
	hdr("To", m.To)
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", time.Now().UTC().Format(time.RFC1123Z))
	hdr("Message-ID", messageID(from))
	hdr("MIME-Version", "1.0")
	if h := unsubscribeHeaders(m); h != nil {
		hdr("List-Unsubscribe", h["List-Unsubscribe"])
		hdr("List-Unsubscribe-Post", h["List-Unsubscribe-Post"])
	}

	if m.HTML == "" {
		hdr("Content-Type", `text/plain; charset="utf-8"`)
		hdr("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		if err := writeQP(&buf, m.Text); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	buf.WriteString("\r\n")
	for _, part := range []struct{ ct, content string }{
		{`text/plain; charset="utf-8"`, m.Text},
		{`text/html; charset="utf-8"`, m.HTML},
	} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ct},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeQP(pw, part.content); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	buf.Write(body.Bytes())
	return buf.Bytes(), nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return err
	}
	return qp.Close()
}

func messageID(from string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	domain := "localhost"
	if a, err := mail.ParseAddress(from); err == nil {
		if at := strings.LastIndexByte(a.Address, '@'); at >= 0 {
			domain = a.Address[at+1:]
		}
	}
	return "<" + hex.EncodeToString(b[:]) + "@" + domain + ">"
}
