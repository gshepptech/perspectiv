package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/user/dbq"
)

// MaxBioRunes bounds a bio, counted in characters after trimming the
// surrounding whitespace.
const MaxBioRunes = 500

// Password confirmation on DeleteAccount shares the login lockout: a wrong
// password records a failed row in login_attempts, and LockoutFailures
// failures inside lockoutWindowMinutes refuse the deletion even with the
// right password. Both match the login lockout, so neither path can be used
// to guess a password faster than the other.
const (
	LockoutFailures      = 10
	lockoutWindowMinutes = 15
)

// maxBlockedListed bounds the blocked list in the owner's Account, newest
// block first. A list longer than this is truncated, not paginated.
const maxBlockedListed = 500

const uniqueViolation = "23505"

// PasswordVerifier checks a password against a stored password hash. The
// hash format belongs to auth; the composition root passes auth's verifier.
// ok is false for a wrong password; err is for a hash that cannot be checked.
type PasswordVerifier interface {
	VerifyPassword(hash, password string) (ok bool, err error)
}

// Service is the user domain's logic over its sqlc queries.
type Service struct {
	pool      *pgxpool.Pool
	q         *dbq.Queries
	avatars   *AvatarStore
	passwords PasswordVerifier
	log       zerolog.Logger
}

// NewService builds the service. All arguments are required.
func NewService(pool *pgxpool.Pool, avatars *AvatarStore, passwords PasswordVerifier, log zerolog.Logger) *Service {
	return &Service{pool: pool, q: dbq.New(pool), avatars: avatars, passwords: passwords, log: log}
}

// Ref is how one user appears to another.
type Ref struct {
	UserID   uuid.UUID
	Username string
}

// Profile is a user's public profile. For a deleted account only UserID,
// Username (the placeholder) and Deleted are set.
type Profile struct {
	UserID    uuid.UUID
	Username  string
	Bio       string
	AvatarURL string
	JoinedAt  time.Time
	Suspended bool
	Deleted   bool
	// CanonCount and LoreCount are the user's badges: the canons and lores
	// (canon included) their readable perspectives belong to.
	CanonCount int32
	LoreCount  int32
	// Followers counts the live accounts following the user. Following is
	// whether the signed-in caller follows them; it is false for the
	// caller's own profile. GetProfile alone sets both.
	Followers int32
	Following bool
}

// Account is the owner's private account state.
type Account struct {
	Email                string
	EmailVerified        bool
	Role                 perspectivv1.Role
	UsernameChangedAt    *time.Time
	NextUsernameChangeAt *time.Time
	Blocked              []Ref
}

// ProfileQuery names one profile: by UserID, by Username (any case), or,
// when both are empty, the caller's own.
type ProfileQuery struct {
	UserID   uuid.UUID
	Username string
}

// GetProfile returns the profile q names, and the Account only when that
// profile is the caller's own. caller is nil for an anonymous request.
func (s *Service) GetProfile(ctx context.Context, caller *uuid.UUID, q ProfileQuery) (Profile, *Account, error) {
	id := q.UserID
	switch {
	case q.Username != "":
		found, err := s.q.GetActiveUserIDByUsername(ctx, q.Username)
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, nil, apiutil.NotFound("user not found")
		}
		if err != nil {
			return Profile{}, nil, apiutil.Internal(fmt.Errorf("find user by username: %w", err))
		}
		id = found
	case id == uuid.Nil:
		if caller == nil {
			return Profile{}, nil, apiutil.Unauthenticated("sign in to see your own profile")
		}
		id = *caller
	}
	own := caller != nil && *caller == id

	u, err := s.q.GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		if own {
			return Profile{}, nil, errAccountGone
		}
		return Profile{}, nil, apiutil.NotFound("user not found")
	}
	if err != nil {
		return Profile{}, nil, apiutil.Internal(fmt.Errorf("get user: %w", err))
	}
	if own && u.DeletedAt != nil {
		return Profile{}, nil, errAccountGone
	}
	p := profileOf(u)
	if !p.Deleted {
		if err := s.liveCounts(ctx, caller, &p); err != nil {
			return Profile{}, nil, err
		}
	}
	if !own {
		return p, nil, nil
	}
	acct, err := s.account(ctx, u)
	if err != nil {
		return Profile{}, nil, err
	}
	return p, acct, nil
}

// liveCounts sets a live profile's badges, its follower count and whether
// the caller follows it.
func (s *Service) liveCounts(ctx context.Context, caller *uuid.UUID, p *Profile) error {
	counts, err := s.q.LoreCounts(ctx, p.UserID)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("count lores: %w", err))
	}
	p.CanonCount, p.LoreCount = counts.CanonCount, counts.LoreCount
	return s.followInfo(ctx, caller, p)
}

// errAccountGone answers a caller whose account no longer exists: their
// access token outlived the account.
var errAccountGone = apiutil.Unauthenticated("account no longer exists")

func profileOf(u dbq.GetUserRow) Profile {
	if u.DeletedAt != nil {
		return Profile{UserID: u.ID, Username: DeletedUsername, Deleted: true}
	}
	return Profile{
		UserID:    u.ID,
		Username:  u.Username,
		Bio:       u.Bio,
		AvatarURL: avatarURL(u.AvatarPath),
		JoinedAt:  u.CreatedAt,
		Suspended: u.SuspendedAt != nil,
	}
}

func (s *Service) account(ctx context.Context, u dbq.GetUserRow) (*Account, error) {
	role, err := roleFromDB(u.Role)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	acct := &Account{
		Email:             u.Email,
		EmailVerified:     u.EmailVerifiedAt != nil,
		Role:              role,
		UsernameChangedAt: u.UsernameChangedAt,
	}
	if u.UsernameChangedAt != nil {
		next := nextUsernameChange(*u.UsernameChangedAt)
		if next.After(time.Now()) {
			acct.NextUsernameChangeAt = &next
		}
	}
	rows, err := s.q.ListBlocked(ctx, dbq.ListBlockedParams{BlockerID: u.ID, MaxRows: maxBlockedListed})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list blocked: %w", err))
	}
	acct.Blocked = make([]Ref, 0, len(rows))
	for _, r := range rows {
		acct.Blocked = append(acct.Blocked, Ref{UserID: r.ID, Username: PublicUsername(r.Username, r.DeletedAt != nil)})
	}
	return acct, nil
}

func nextUsernameChange(changedAt time.Time) time.Time {
	return changedAt.Add(usernameChangeWindow)
}

// requireActive refuses a caller whose account is deleted or missing.
func (s *Service) requireActive(ctx context.Context, q *dbq.Queries, caller uuid.UUID) error {
	ok, err := q.UserIsActive(ctx, caller)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("check account: %w", err))
	}
	if !ok {
		return errAccountGone
	}
	return nil
}

func (s *Service) ownProfile(ctx context.Context, caller uuid.UUID) (Profile, error) {
	u, err := s.q.GetUser(ctx, caller)
	if err != nil {
		return Profile{}, apiutil.Internal(fmt.Errorf("get user: %w", err))
	}
	return profileOf(u), nil
}

// ProfileUpdate is an UpdateProfile request. Bio always replaces the stored
// bio (proto3 cannot tell an unset string from an empty one, so an empty bio
// clears it). Avatar, when non-empty, replaces the avatar; otherwise
// ClearAvatar removes it; otherwise the avatar is unchanged.
type ProfileUpdate struct {
	Bio               string
	Avatar            []byte
	AvatarContentType string
	ClearAvatar       bool
}

// UpdateProfile applies in to the caller's profile.
func (s *Service) UpdateProfile(ctx context.Context, caller uuid.UUID, in ProfileUpdate) (Profile, error) {
	bio := strings.TrimSpace(in.Bio)
	if utf8.RuneCountInString(bio) > MaxBioRunes {
		return Profile{}, apiutil.InvalidArg(fmt.Sprintf("bio must be at most %d characters", MaxBioRunes))
	}
	var ext string
	var img []byte
	if len(in.Avatar) > 0 {
		var err error
		if ext, img, err = normalizeAvatar(in.Avatar, in.AvatarContentType); err != nil {
			return Profile{}, err
		}
	}

	var newFile string
	if img != nil {
		name, err := s.avatars.write(ext, img)
		if err != nil {
			return Profile{}, apiutil.Internal(err)
		}
		newFile = name
	}
	oldFile, err := s.updateProfileTx(ctx, caller, bio, newFile, in.ClearAvatar)
	if err != nil {
		if newFile != "" {
			s.removeAvatar(caller, newFile)
		}
		return Profile{}, err
	}
	if oldFile != "" {
		s.removeAvatar(caller, oldFile)
	}
	return s.ownProfile(ctx, caller)
}

// updateProfileTx writes the bio and avatar path and returns the avatar file
// the update replaced or cleared ("" for none).
func (s *Service) updateProfileTx(ctx context.Context, caller uuid.UUID, bio, newFile string, clear bool) (string, error) {
	var replaced string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.LockUser(ctx, caller)
		if errors.Is(err, pgx.ErrNoRows) {
			return errAccountGone
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("lock user: %w", err))
		}
		if row.DeletedAt != nil {
			return errAccountGone
		}
		path := row.AvatarPath
		switch {
		case newFile != "":
			path = &newFile
		case clear:
			path = nil
		}
		if row.AvatarPath != nil && (path == nil || *path != *row.AvatarPath) {
			replaced = *row.AvatarPath
		}
		if err := q.UpdateProfile(ctx, dbq.UpdateProfileParams{ID: caller, Bio: bio, AvatarPath: path}); err != nil {
			return apiutil.Internal(fmt.Errorf("update profile: %w", err))
		}
		return nil
	})
	return replaced, err
}

// removeAvatar deletes a file no row points at any more. A failure leaves an
// orphan file that no URL reaches; it is logged, not returned, because the
// database write it follows has already committed.
func (s *Service) removeAvatar(caller uuid.UUID, name string) {
	if err := s.avatars.remove(name); err != nil {
		s.log.Error().Err(err).Str("event", "avatar_remove_failed").Str("sub", caller.String()).Str("file", name).Send()
	}
}

// ChangeUsername sets the caller's username, at most once per
// UsernameChangeWindowDays. It returns the new profile and the earliest time
// of the next change.
func (s *Service) ChangeUsername(ctx context.Context, caller uuid.UUID, name string) (Profile, time.Time, error) {
	if err := ValidateUsername(name); err != nil {
		return Profile{}, time.Time{}, err
	}
	u, err := s.q.GetUser(ctx, caller)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, time.Time{}, errAccountGone
	}
	if err != nil {
		return Profile{}, time.Time{}, apiutil.Internal(fmt.Errorf("get user: %w", err))
	}
	if u.DeletedAt != nil {
		return Profile{}, time.Time{}, errAccountGone
	}
	// A change of case only is a change; an identical name is not.
	if u.Username == name {
		return Profile{}, time.Time{}, apiutil.InvalidArg("that is already your username")
	}

	changedAt, err := s.q.ChangeUsername(ctx, dbq.ChangeUsernameParams{ID: caller, Username: name, WindowHours: int32(usernameChangeWindow / time.Hour)})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation:
		return Profile{}, time.Time{}, apiutil.AlreadyExists("that username is taken")
	case errors.Is(err, pgx.ErrNoRows):
		return Profile{}, time.Time{}, s.changeRefused(ctx, caller)
	case err != nil:
		return Profile{}, time.Time{}, apiutil.Internal(fmt.Errorf("change username: %w", err))
	case changedAt == nil:
		return Profile{}, time.Time{}, apiutil.Internal(errors.New("change username returned no time"))
	}
	p, err := s.ownProfile(ctx, caller)
	if err != nil {
		return Profile{}, time.Time{}, err
	}
	return p, nextUsernameChange(*changedAt), nil
}

// changeRefused explains why the guarded username update matched no row.
func (s *Service) changeRefused(ctx context.Context, caller uuid.UUID) error {
	u, err := s.q.GetUser(ctx, caller)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("get user: %w", err))
	}
	if u.DeletedAt != nil || u.UsernameChangedAt == nil {
		return errAccountGone
	}
	next := nextUsernameChange(*u.UsernameChangedAt)
	return apiutil.FailedPrecondition(fmt.Sprintf("username can be changed once every %d days; next change after %s",
		UsernameChangeWindowDays, next.UTC().Format(time.RFC3339)))
}

// BlockUser records that the caller has blocked target and, in the same
// transaction, removes any follow between them in either direction.
// Blocking an already blocked user succeeds and changes nothing.
func (s *Service) BlockUser(ctx context.Context, caller, target uuid.UUID) error {
	if caller == target {
		return apiutil.InvalidArg("you cannot block yourself")
	}
	if err := s.requireActive(ctx, s.q, caller); err != nil {
		return err
	}
	ok, err := s.q.UserIsActive(ctx, target)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("check target: %w", err))
	}
	if !ok {
		return apiutil.NotFound("user not found")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockPair(ctx, dbq.LockPairParams{A: caller, B: target}); err != nil {
			return apiutil.Internal(fmt.Errorf("lock pair: %w", err))
		}
		if err := q.InsertBlock(ctx, dbq.InsertBlockParams{BlockerID: caller, BlockedID: target}); err != nil {
			return apiutil.Internal(fmt.Errorf("insert block: %w", err))
		}
		if err := q.DeleteFollowsBetween(ctx, dbq.DeleteFollowsBetweenParams{A: caller, B: target}); err != nil {
			return apiutil.Internal(fmt.Errorf("delete follows: %w", err))
		}
		return nil
	})
}

// UnblockUser removes the caller's block on target. Unblocking a user who is
// not blocked, or who does not exist, succeeds and changes nothing.
func (s *Service) UnblockUser(ctx context.Context, caller, target uuid.UUID) error {
	if err := s.requireActive(ctx, s.q, caller); err != nil {
		return err
	}
	if err := s.q.DeleteBlock(ctx, dbq.DeleteBlockParams{BlockerID: caller, BlockedID: target}); err != nil {
		return apiutil.Internal(fmt.Errorf("delete block: %w", err))
	}
	return nil
}

// DeleteAccount soft-deletes the caller's account after checking password.
//
// In one transaction it soft-deletes every perspective the account wrote
// (drafts too), marks the arcs those sat in for the next rank run (which
// dissolves their claims and recomputes lore and canon), erases the
// account's email, username, bio, avatar and password hash, removes every
// follow it made or received, and revokes its sessions and refresh tokens. The avatar file is removed after the commit.
func (s *Service) DeleteAccount(ctx context.Context, caller uuid.UUID, password string) error {
	u, err := s.q.GetUser(ctx, caller)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAccountGone
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("get user: %w", err))
	}
	if u.DeletedAt != nil {
		return errAccountGone
	}
	if err := s.checkPassword(ctx, u, password); err != nil {
		return err
	}

	var avatar string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		avatar, err = s.deleteAccountTx(ctx, s.q.WithTx(tx), caller)
		return err
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "account_deleted").Str("sub", caller.String()).Send()
	if avatar != "" {
		s.removeAvatar(caller, avatar)
	}
	return nil
}

// deleteAccountTx is DeleteAccount's transaction. It returns the avatar file
// to remove once the transaction commits.
func (s *Service) deleteAccountTx(ctx context.Context, q *dbq.Queries, caller uuid.UUID) (string, error) {
	row, err := q.LockUser(ctx, caller)
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("lock user: %w", err))
	}
	if row.DeletedAt != nil {
		return "", errAccountGone
	}
	arcs, err := q.SoftDeletePerspectivesByAuthor(ctx, caller)
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("delete perspectives: %w", err))
	}
	if len(arcs) > 0 {
		if err := q.MarkArcsLoreDirty(ctx, arcs); err != nil {
			return "", apiutil.Internal(fmt.Errorf("mark arcs: %w", err))
		}
	}
	if err := q.DeleteFollowsOf(ctx, caller); err != nil {
		return "", apiutil.Internal(fmt.Errorf("delete follows: %w", err))
	}
	tag := strings.ReplaceAll(caller.String(), "-", "")
	if err := q.AnonymizeUser(ctx, dbq.AnonymizeUserParams{
		ID:       caller,
		Email:    tag + "@deleted.invalid",
		Username: deletedUsernamePrefix + tag,
	}); err != nil {
		return "", apiutil.Internal(fmt.Errorf("anonymize user: %w", err))
	}
	if err := q.RevokeRefreshTokens(ctx, caller); err != nil {
		return "", apiutil.Internal(fmt.Errorf("revoke refresh tokens: %w", err))
	}
	if err := q.RevokeSessions(ctx, caller); err != nil {
		return "", apiutil.Internal(fmt.Errorf("revoke sessions: %w", err))
	}
	var avatar string
	if row.AvatarPath != nil {
		avatar = *row.AvatarPath
	}
	return avatar, nil
}

// checkPassword applies the shared lockout, then verifies password. A wrong
// password is recorded before it is refused, and the record is written
// synchronously: a failure to record it refuses the call.
func (s *Service) checkPassword(ctx context.Context, u dbq.GetUserRow, password string) error {
	failures, err := s.q.CountRecentFailedAttempts(ctx, dbq.CountRecentFailedAttemptsParams{
		UserID: u.ID, WindowMinutes: lockoutWindowMinutes,
	})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("count failed attempts: %w", err))
	}
	if failures >= LockoutFailures {
		return apiutil.ResourceExhausted("too many wrong passwords; try again later")
	}
	ok, err := s.passwords.VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("verify password: %w", err))
	}
	if ok {
		return nil
	}
	if err := s.q.InsertFailedAttempt(ctx, u.ID); err != nil {
		return apiutil.Internal(fmt.Errorf("record failed attempt: %w", err))
	}
	s.log.Warn().Str("event", "delete_account_bad_password").Str("sub", u.ID.String()).Send()
	return apiutil.PermissionDenied("password is incorrect")
}

// ExportMyData returns everything the database holds about the caller's
// account as one JSON document, built by ExportAccount.
func (s *Service) ExportMyData(ctx context.Context, caller uuid.UUID) (string, error) {
	doc, err := s.q.ExportAccount(ctx, caller)
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("export account: %w", err))
	}
	s.log.Info().Str("event", "data_export").Str("sub", caller.String()).Msg("account data exported")
	return doc, nil
}
