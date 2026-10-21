package db_test

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// valueNames returns an enum's value names in number order.
func valueNames(e protoreflect.EnumDescriptor) []string {
	vals := e.Values()
	names := make([]string, 0, vals.Len())
	for i := range vals.Len() {
		names = append(names, string(vals.Get(i).Name()))
	}
	return names
}

// TestEnumValues pins the enum values the spec fixes (OT-007, AC-007, AC-015)
// and the two sets the spec leaves open: the six emoji and the report reasons.
func TestEnumValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		enum protoreflect.EnumDescriptor
		want []string
	}{
		{
			name: "OT-007 AC-007 place kind is exactly the five levels",
			enum: perspectivv1.PlaceKind(0).Descriptor(),
			want: []string{"PLACE_KIND_UNSPECIFIED", "PLACE_KIND_STATE", "PLACE_KIND_CITY", "PLACE_KIND_TOWN", "PLACE_KIND_MALL", "PLACE_KIND_STORE"},
		},
		{
			name: "place source",
			enum: perspectivv1.PlaceSource(0).Descriptor(),
			want: []string{"PLACE_SOURCE_UNSPECIFIED", "PLACE_SOURCE_OVERTURE", "PLACE_SOURCE_FOURSQUARE", "PLACE_SOURCE_USER"},
		},
		{
			name: "AC-015 FR-017 genre is the fixed seven",
			enum: perspectivv1.Genre(0).Descriptor(),
			want: []string{"GENRE_UNSPECIFIED", "GENRE_DRAMA", "GENRE_COMEDY", "GENRE_ROMANCE", "GENRE_MYSTERY", "GENRE_HORROR", "GENRE_SLICE_OF_LIFE", "GENRE_ACTION"},
		},
		{
			name: "role",
			enum: perspectivv1.Role(0).Descriptor(),
			want: []string{"ROLE_UNSPECIFIED", "ROLE_USER", "ROLE_ADMIN"},
		},
		{
			name: "perspective status",
			enum: perspectivv1.PerspectiveStatus(0).Descriptor(),
			want: []string{"PERSPECTIVE_STATUS_UNSPECIFIED", "PERSPECTIVE_STATUS_DRAFT", "PERSPECTIVE_STATUS_PUBLISHED"},
		},
		{
			name: "lore status",
			enum: perspectivv1.LoreStatus(0).Descriptor(),
			want: []string{"LORE_STATUS_UNSPECIFIED", "LORE_STATUS_PENDING", "LORE_STATUS_IN_LORE", "LORE_STATUS_SOLO", "LORE_STATUS_REMOVED"},
		},
		{
			name: "claim status",
			enum: perspectivv1.ClaimStatus(0).Descriptor(),
			want: []string{"CLAIM_STATUS_UNSPECIFIED", "CLAIM_STATUS_PENDING", "CLAIM_STATUS_ACCEPTED", "CLAIM_STATUS_REJECTED"},
		},
		{
			name: "lore standing",
			enum: perspectivv1.LoreStanding(0).Descriptor(),
			want: []string{"LORE_STANDING_UNSPECIFIED", "LORE_STANDING_CANON", "LORE_STANDING_SUBLORE"},
		},
		{
			name: "emoji is six reacts: heart laugh wow sad fire clap",
			enum: perspectivv1.Emoji(0).Descriptor(),
			want: []string{"EMOJI_UNSPECIFIED", "EMOJI_HEART", "EMOJI_LAUGH", "EMOJI_WOW", "EMOJI_SAD", "EMOJI_FIRE", "EMOJI_CLAP"},
		},
		{
			name: "notification kind is the inbox events",
			enum: perspectivv1.NotificationKind(0).Descriptor(),
			want: []string{
				"NOTIFICATION_KIND_UNSPECIFIED", "NOTIFICATION_KIND_CLAIM_RECEIVED", "NOTIFICATION_KIND_CLAIM_ACCEPTED",
				"NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC", "NOTIFICATION_KIND_VOTE_OUT_OPENED",
				"NOTIFICATION_KIND_CANON_CHANGE", "NOTIFICATION_KIND_REPORT_RESOLVED",
				"NOTIFICATION_KIND_INVITE_ANSWERED", "NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE",
			},
		},
		{
			name: "report target kind",
			enum: perspectivv1.ReportTargetKind(0).Descriptor(),
			want: []string{"REPORT_TARGET_KIND_UNSPECIFIED", "REPORT_TARGET_KIND_PERSPECTIVE", "REPORT_TARGET_KIND_PLACE", "REPORT_TARGET_KIND_USER"},
		},
		{
			name: "report reason leads with real name or handle of a non-user",
			enum: perspectivv1.ReportReason(0).Descriptor(),
			want: []string{
				"REPORT_REASON_UNSPECIFIED", "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER", "REPORT_REASON_HARASSMENT",
				"REPORT_REASON_HATEFUL_CONTENT", "REPORT_REASON_SEXUAL_CONTENT", "REPORT_REASON_SPAM", "REPORT_REASON_OTHER",
			},
		},
		{
			name: "report status",
			enum: perspectivv1.ReportStatus(0).Descriptor(),
			want: []string{"REPORT_STATUS_UNSPECIFIED", "REPORT_STATUS_OPEN", "REPORT_STATUS_RESOLVED"},
		},
		{
			name: "moderation action",
			enum: perspectivv1.ModerationAction(0).Descriptor(),
			want: []string{"MODERATION_ACTION_UNSPECIFIED", "MODERATION_ACTION_HIDE_PERSPECTIVE", "MODERATION_ACTION_HIDE_PLACE", "MODERATION_ACTION_SUSPEND_USER", "MODERATION_ACTION_DISMISS"},
		},
		{
			name: "epic tab",
			enum: perspectivv1.EpicTab(0).Descriptor(),
			want: []string{"EPIC_TAB_UNSPECIFIED", "EPIC_TAB_RANKED", "EPIC_TAB_NEWEST", "EPIC_TAB_TALES"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := valueNames(tt.enum)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("values = %v, want %v", got, tt.want)
			}
			for i := range tt.enum.Values().Len() {
				if n := tt.enum.Values().Get(i).Number(); int(n) != i {
					t.Errorf("%s has number %d, want %d", tt.enum.Values().Get(i).Name(), n, i)
				}
			}
		})
	}
}

// services lists every service of the API with the RPCs the contract fixes.
// ListByAuthor serves the profile's perspectives and tales (AC-023), and
// ListArcSection the arc page's later pages (NFR-008). CreateInvite and
// GetInvite serve the "I was there too" share link, ListLorePosts and
// AddLorePost a lore's thread, NameLore a lore's name, and FollowPlace and
// ListFollowing the places a user follows.
var services = map[string][]string{
	"AuthService":        {"SignUp", "Login", "Logout", "Refresh", "VerifyEmail", "RequestPasswordReset", "ResetPassword"},
	"UserService":        {"GetProfile", "UpdateProfile", "ChangeUsername", "BlockUser", "UnblockUser", "DeleteAccount"},
	"PlaceService":       {"SearchPlaces", "GetPlace", "ListChildren", "CreatePlace", "GetEpic", "FollowPlace", "ListFollowing"},
	"ArcService":         {"GetArc", "ListArcSection", "RetitleHourArc"},
	"PerspectiveService": {"SaveDraft", "Publish", "Update", "Delete", "Get", "ListMine", "ListByAuthor", "CreateInvite", "GetInvite"},
	"LoreService":        {"ClaimCharacter", "ResolveClaim", "ListClaims", "GetLore", "ListLorePosts", "AddLorePost", "NameLore"},
	"VoteService":        {"Vote", "VoteRemove"},
	"ReactService":       {"React"},
	"NotifyService":      {"ListInbox", "MarkRead", "GetPreferences", "UpdatePreferences"},
	"ModerationService":  {"Report", "ListReports", "Resolve"},
}

func service(t *testing.T, name string) protoreflect.ServiceDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName("perspectiv.v1." + name))
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("%s is a %T, not a service", name, d)
	}
	return sd
}

func TestServicesExposeExactlyTheContractRPCs(t *testing.T) {
	t.Parallel()
	for name, want := range services {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sd := service(t, name)
			var got []string
			for i := range sd.Methods().Len() {
				got = append(got, string(sd.Methods().Get(i).Name()))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%s RPCs = %v, want %v", name, got, want)
			}
		})
	}
}

// emailFieldsReachable walks every message reachable from m and returns the
// dotted paths of fields whose name contains "email" and whose type is a
// string, so a boolean such as email_verified is not an address.
func emailFieldsReachable(m protoreflect.MessageDescriptor, path string, seen map[protoreflect.FullName]bool) []string {
	if seen[m.FullName()] {
		return nil
	}
	seen[m.FullName()] = true
	defer delete(seen, m.FullName())
	var out []string
	fields := m.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		p := path + "." + string(f.Name())
		if f.Kind() == protoreflect.StringKind && strings.Contains(string(f.Name()), "email") {
			out = append(out, p)
		}
		if f.Message() != nil {
			out = append(out, emailFieldsReachable(f.Message(), p, seen)...)
		}
	}
	return out
}

// TestNoResponseCarriesAnEmailExceptTheCallersOwnAccount covers FR-003 and
// OT-004 at the wire contract: the only email field reachable from any RPC
// response is Account.email, and Account is reachable only from the
// caller's own profile.
func TestNoResponseCarriesAnEmailExceptTheCallersOwnAccount(t *testing.T) {
	t.Parallel()
	allowed := []string{"UserService.GetProfile:GetProfileResponse.account.email"}
	var got []string
	for name := range services {
		sd := service(t, name)
		for i := range sd.Methods().Len() {
			md := sd.Methods().Get(i)
			out := md.Output()
			prefix := name + "." + string(md.Name()) + ":" + string(out.Name())
			got = append(got, emailFieldsReachable(out, prefix, map[protoreflect.FullName]bool{})...)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, allowed) {
		t.Fatalf("email fields reachable from responses = %v, want only %v", got, allowed)
	}
}

// TestGlossaryVocabulary covers FR-041: the wire types carry the product
// glossary's names.
func TestGlossaryVocabulary(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"Place", "Arc", "Perspective", "Part", "Character", "Claim", "Encounter", "Lore", "EpicArc", "EpicTale",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName("perspectiv.v1." + name))
			if err != nil {
				t.Fatalf("no message perspectiv.v1.%s: %v", name, err)
			}
			if _, ok := d.(protoreflect.MessageDescriptor); !ok {
				t.Fatalf("perspectiv.v1.%s is a %T, not a message", name, d)
			}
		})
	}
	standing := valueNames(perspectivv1.LoreStanding(0).Descriptor())
	if !slices.Contains(standing, "LORE_STANDING_CANON") || !slices.Contains(standing, "LORE_STANDING_SUBLORE") {
		t.Errorf("LoreStanding = %v, want CANON and SUBLORE", standing)
	}
	if !slices.Contains(valueNames(perspectivv1.EpicTab(0).Descriptor()), "EPIC_TAB_TALES") {
		t.Error("EpicTab has no TALES tab")
	}
}

// TestPerspectiveCarriesOneGenre covers FR-017 and AC-015 on the wire: a
// perspective and every write request carry a single, non-repeated genre.
func TestPerspectiveCarriesOneGenre(t *testing.T) {
	t.Parallel()
	for _, m := range []protoreflect.MessageDescriptor{
		(&perspectivv1.Perspective{}).ProtoReflect().Descriptor(),
		(&perspectivv1.PerspectiveSummary{}).ProtoReflect().Descriptor(),
		(&perspectivv1.SaveDraftRequest{}).ProtoReflect().Descriptor(),
		(&perspectivv1.PublishRequest{}).ProtoReflect().Descriptor(),
		(&perspectivv1.PerspectiveServiceUpdateRequest{}).ProtoReflect().Descriptor(),
	} {
		t.Run(string(m.Name()), func(t *testing.T) {
			t.Parallel()
			f := m.Fields().ByName("genre")
			if f == nil {
				t.Fatal("no genre field")
			}
			if f.Cardinality() == protoreflect.Repeated || f.Enum() == nil || f.Enum().FullName() != "perspectiv.v1.Genre" {
				t.Fatalf("genre is %v %v, want a single perspectiv.v1.Genre", f.Cardinality(), f.Kind())
			}
		})
	}
}
