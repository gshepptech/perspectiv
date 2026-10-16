package mailer

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMIME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		msg       Message
		wantParts map[string]string
	}{
		{
			name:      "text only",
			msg:       Message{To: "a@example.test", Subject: "Hello", Text: "plain body"},
			wantParts: map[string]string{"text/plain": "plain body"},
		},
		{
			name: "text and html alternatives",
			msg:  Message{To: "a@example.test", Subject: "Ünïcode subject", Text: "plain body", HTML: "<p>html body</p>"},
			wantParts: map[string]string{
				"text/plain": "plain body",
				"text/html":  "<p>html body</p>",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := buildMIME("Perspectiv <no-reply@perspectiv.test>", tc.msg)
			if err != nil {
				t.Fatalf("buildMIME: %v", err)
			}
			parsed, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			checkHeaders(t, parsed, tc.msg)
			got := readParts(t, parsed)
			for ct, body := range tc.wantParts {
				if strings.TrimSpace(got[ct]) != body {
					t.Fatalf("%s part = %q, want %q", ct, got[ct], body)
				}
			}
			if len(got) != len(tc.wantParts) {
				t.Fatalf("parts = %v, want %d", got, len(tc.wantParts))
			}
		})
	}
}

func checkHeaders(t *testing.T, parsed *mail.Message, msg Message) {
	t.Helper()
	subj, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subj != msg.Subject {
		t.Fatalf("Subject = %q (%v), want %q", subj, err, msg.Subject)
	}
	if got := parsed.Header.Get("To"); !strings.Contains(got, msg.To) {
		t.Fatalf("To = %q", got)
	}
	if parsed.Header.Get("Message-ID") == "" || parsed.Header.Get("Date") == "" {
		t.Fatal("Message-ID and Date are required")
	}
}

func readParts(t *testing.T, m *mail.Message) map[string]string {
	t.Helper()
	ct, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content type: %v", err)
	}
	out := map[string]string{}
	if !strings.HasPrefix(ct, "multipart/") {
		out[ct] = decodeQP(t, m.Header.Get("Content-Transfer-Encoding"), m.Body)
		return out
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("part: %v", err)
		}
		pct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		// multipart.Reader decodes quoted-printable parts itself.
		b, _ := io.ReadAll(p)
		out[pct] = string(b)
	}
}

func decodeQP(t *testing.T, enc string, r io.Reader) string {
	t.Helper()
	if !strings.EqualFold(enc, "quoted-printable") {
		b, _ := io.ReadAll(r)
		return string(b)
	}
	b, err := io.ReadAll(quotedprintable.NewReader(r))
	if err != nil {
		t.Fatalf("qp: %v", err)
	}
	return string(b)
}

func TestBuildMIMERefusesHeaderInjection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from string
		msg  Message
	}{
		{name: "subject", from: "a@example.test", msg: Message{To: "b@example.test", Subject: "x\nBcc: c@example.test", Text: "t"}},
		{name: "recipient", from: "a@example.test", msg: Message{To: "b@example.test\r\nBcc: c@example.test", Subject: "x", Text: "t"}},
		{name: "sender", from: "a@example.test\nBcc: c@example.test", msg: Message{To: "b@example.test", Subject: "x", Text: "t"}},
		{name: "empty body", from: "a@example.test", msg: Message{To: "b@example.test", Subject: "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildMIME(tc.from, tc.msg); err == nil {
				t.Fatal("buildMIME = nil error, want refusal")
			}
		})
	}
}
