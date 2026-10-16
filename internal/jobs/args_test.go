package jobs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
)

func TestArgsKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "expiry", got: jobs.ExpiryArgs{}.Kind(), want: "perspective_expiry"},
		{name: "rank", got: jobs.RankArgs{}.Kind(), want: "lore_rank"},
		{name: "email", got: jobs.EmailArgs{}.Kind(), want: "email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Fatalf("Kind() = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// River stores args as JSON and decodes them for the worker, so every field
// has to survive the round trip.
func TestArgsJSONRoundTrip(t *testing.T) {
	t.Parallel()
	notify := jobs.NotifyArgs{
		Kind:          perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED,
		ActorID:       uuid.New(),
		ArcID:         uuid.New(),
		PerspectiveID: uuid.New(),
		ClaimID:       uuid.New(),
		LoreID:        uuid.Nil,
		ReportID:      uuid.Nil,
	}
	email := jobs.EmailArgs{To: "a@example.test", Template: "verify_email", Data: map[string]string{"token": "t"}}
	expiry := jobs.ExpiryArgs{PerspectiveID: uuid.New()}

	cases := []struct {
		name string
		in   any
		out  any
		cmp  func(out any) bool
	}{
		{name: "notify", in: notify, out: &jobs.NotifyArgs{}, cmp: func(o any) bool { return *o.(*jobs.NotifyArgs) == notify }},
		{name: "email", in: email, out: &jobs.EmailArgs{}, cmp: func(o any) bool {
			e := o.(*jobs.EmailArgs)
			return e.To == email.To && e.Template == email.Template && e.Data["token"] == "t" && len(e.Data) == 1
		}},
		{name: "expiry", in: expiry, out: &jobs.ExpiryArgs{}, cmp: func(o any) bool { return *o.(*jobs.ExpiryArgs) == expiry }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := json.Unmarshal(raw, tc.out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !tc.cmp(tc.out) {
				t.Fatalf("round trip of %s lost data: %s", tc.name, raw)
			}
		})
	}
}

// Invalid arguments are refused before the transaction is touched, so a nil
// transaction and an enqueuer with no client are enough to reach the check.
func TestEnqueuerRejectsInvalidArgs(t *testing.T) {
	t.Parallel()
	e := jobs.NewEnqueuer(nil)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{name: "expiry nil perspective", call: func() error { return e.ExpiryTx(ctx, nil, uuid.Nil, time.Now()) }},
		{name: "expiry zero run time", call: func() error { return e.ExpiryTx(ctx, nil, uuid.New(), time.Time{}) }},
		{name: "email no recipient", call: func() error {
			return e.EmailTx(ctx, nil, jobs.EmailArgs{Template: jobs.TemplateVerifyEmail})
		}},
		{name: "email unknown template", call: func() error {
			return e.EmailTx(ctx, nil, jobs.EmailArgs{To: "a@example.test", Template: "welcome"})
		}},
		{name: "notify unspecified kind", call: func() error {
			return e.NotifyTx(ctx, nil, jobs.NotifyArgs{ArcID: uuid.New()})
		}},
		{name: "notify undefined kind", call: func() error {
			return e.NotifyTx(ctx, nil, jobs.NotifyArgs{Kind: perspectivv1.NotificationKind(99)})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatal("expected an error for invalid arguments, got nil")
			}
		})
	}
}

func TestDefaultExpiryWindow(t *testing.T) {
	t.Parallel()
	if jobs.DefaultExpiryWindow != 7*24*time.Hour {
		t.Fatalf("DefaultExpiryWindow = %s, want 168h", jobs.DefaultExpiryWindow)
	}
}
