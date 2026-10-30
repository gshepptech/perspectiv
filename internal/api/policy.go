// Package api holds the HTTP surface every domain shares: the route policy,
// the Connect interceptor chain that enforces it, and the chi router with the
// health, metrics, build-info and SPA routes.
package api

// Access is the authorization class a procedure requires.
type Access int

// Access classes, weakest first.
const (
	Public Access = iota
	User
	Admin
)

func (a Access) String() string {
	switch a {
	case Public:
		return "public"
	case User:
		return "user"
	case Admin:
		return "admin"
	}
	return "unknown"
}

// Policy is what the authorization interceptor enforces for one procedure.
type Policy struct {
	Access Access
	// Verified passes the call through the shared write gate: the caller's
	// email must be verified and the account must not be suspended. Every
	// write that puts or changes text other people read carries it, so a
	// suspended account can neither publish nor edit what it already
	// published.
	Verified bool
	// RateLimit names the limiter action the call counts against; "" means
	// none. One of the Limit* constants.
	RateLimit string
}

// Rate-limit actions passed to Limiter.Allow.
const (
	LimitSignup  = "signup"
	LimitLogin   = "login"
	LimitPublish = "publish"
	LimitClaim   = "claim"
	LimitVote    = "vote"
	LimitReact   = "react"
	LimitReport  = "report"
	LimitPost    = "post"
	LimitSearch  = "search"
	LimitExport  = "export"
)

// Routes maps every Connect procedure to its policy. A procedure missing here
// is denied, so a new RPC is unreachable until it is given a policy.
//
// Logout and Refresh are Public because they authenticate with the refresh
// cookie inside the handler, not with an access token.
var Routes = map[string]Policy{
	"/perspectiv.v1.AuthService/SignUp":               {Access: Public, RateLimit: LimitSignup},
	"/perspectiv.v1.AuthService/Login":                {Access: Public, RateLimit: LimitLogin},
	"/perspectiv.v1.AuthService/ChangePassword":       {Access: User, RateLimit: LimitLogin},
	"/perspectiv.v1.AuthService/RequestEmailChange":   {Access: User, RateLimit: LimitLogin},
	"/perspectiv.v1.AuthService/ConfirmEmailChange":   {Access: Public},
	"/perspectiv.v1.AuthService/SignOutEverywhere":    {Access: User},
	"/perspectiv.v1.AuthService/ListSessions":         {Access: User},
	"/perspectiv.v1.AuthService/RevokeSession":        {Access: User},
	"/perspectiv.v1.AuthService/GetSignUpInfo":        {Access: Public},
	"/perspectiv.v1.AuthService/JoinWaitlist":         {Access: Public, RateLimit: LimitSignup},
	"/perspectiv.v1.AuthService/CreateAccessCode":     {Access: Admin},
	"/perspectiv.v1.AuthService/ListAccessCodes":      {Access: Admin},
	"/perspectiv.v1.AuthService/RevokeAccessCode":     {Access: Admin},
	"/perspectiv.v1.AuthService/ListWaitlist":         {Access: Admin},
	"/perspectiv.v1.AuthService/InviteFromWaitlist":   {Access: Admin},
	"/perspectiv.v1.AuthService/Logout":               {Access: Public},
	"/perspectiv.v1.AuthService/Refresh":              {Access: Public},
	"/perspectiv.v1.AuthService/VerifyEmail":          {Access: Public},
	"/perspectiv.v1.AuthService/RequestPasswordReset": {Access: Public},
	"/perspectiv.v1.AuthService/ResetPassword":        {Access: Public},

	"/perspectiv.v1.UserService/GetProfile":     {Access: Public},
	"/perspectiv.v1.UserService/UpdateProfile":  {Access: User, Verified: true},
	"/perspectiv.v1.UserService/ChangeUsername": {Access: User, Verified: true},
	"/perspectiv.v1.UserService/BlockUser":      {Access: User},
	"/perspectiv.v1.UserService/UnblockUser":    {Access: User},
	"/perspectiv.v1.UserService/DeleteAccount":  {Access: User},
	"/perspectiv.v1.UserService/ExportMyData":   {Access: User, RateLimit: LimitExport},

	"/perspectiv.v1.PlaceService/SearchPlaces":     {Access: Public},
	"/perspectiv.v1.PlaceService/GetPlace":         {Access: Public},
	"/perspectiv.v1.PlaceService/ListChildren":     {Access: Public},
	"/perspectiv.v1.PlaceService/CreatePlace":      {Access: User, Verified: true},
	"/perspectiv.v1.PlaceService/GetEpic":          {Access: Public},
	"/perspectiv.v1.PlaceService/FollowPlace":      {Access: User},
	"/perspectiv.v1.PlaceService/ListFollowing":    {Access: User},
	"/perspectiv.v1.PlaceService/SearchCharacters": {Access: Public, RateLimit: LimitSearch},

	"/perspectiv.v1.ArcService/GetArc":             {Access: Public},
	"/perspectiv.v1.ArcService/FindDayArc":         {Access: Public},
	"/perspectiv.v1.ArcService/ListArcSection":     {Access: Public},
	"/perspectiv.v1.ArcService/ListOpenCharacters": {Access: Public},
	"/perspectiv.v1.ArcService/RetitleHourArc":     {Access: User, Verified: true},

	"/perspectiv.v1.PerspectiveService/SaveDraft":    {Access: User},
	"/perspectiv.v1.PerspectiveService/Publish":      {Access: User, Verified: true, RateLimit: LimitPublish},
	"/perspectiv.v1.PerspectiveService/Update":       {Access: User, Verified: true},
	"/perspectiv.v1.PerspectiveService/Delete":       {Access: User},
	"/perspectiv.v1.PerspectiveService/Get":          {Access: Public},
	"/perspectiv.v1.PerspectiveService/ListMine":     {Access: User},
	"/perspectiv.v1.PerspectiveService/ListByAuthor": {Access: Public},
	"/perspectiv.v1.PerspectiveService/CreateInvite": {Access: User, Verified: true},
	"/perspectiv.v1.PerspectiveService/GetInvite":    {Access: Public},

	"/perspectiv.v1.LoreService/ClaimCharacter": {Access: User, Verified: true, RateLimit: LimitClaim},
	"/perspectiv.v1.LoreService/ResolveClaim":   {Access: User, Verified: true},
	"/perspectiv.v1.LoreService/ListClaims":     {Access: User},
	"/perspectiv.v1.LoreService/GetLore":        {Access: Public},
	"/perspectiv.v1.LoreService/ListLorePosts":  {Access: Public},
	"/perspectiv.v1.LoreService/AddLorePost":    {Access: User, Verified: true, RateLimit: LimitPost},
	"/perspectiv.v1.LoreService/NameLore":       {Access: User, Verified: true, RateLimit: LimitPost},

	"/perspectiv.v1.VoteService/Vote":       {Access: User, Verified: true, RateLimit: LimitVote},
	"/perspectiv.v1.VoteService/VoteRemove": {Access: User, Verified: true, RateLimit: LimitVote},

	"/perspectiv.v1.ReactService/React": {Access: User, Verified: true, RateLimit: LimitReact},

	"/perspectiv.v1.NotifyService/ListInbox":         {Access: User},
	"/perspectiv.v1.NotifyService/MarkRead":          {Access: User},
	"/perspectiv.v1.NotifyService/GetPreferences":    {Access: User},
	"/perspectiv.v1.NotifyService/UpdatePreferences": {Access: User},
	"/perspectiv.v1.NotifyService/Unsubscribe":       {Access: Public},

	"/perspectiv.v1.ModerationService/Report":      {Access: User, Verified: true, RateLimit: LimitReport},
	"/perspectiv.v1.ModerationService/ListReports": {Access: Admin},
	"/perspectiv.v1.ModerationService/Resolve":     {Access: Admin},
	// Admin only; the search limit bounds probing for addresses with a
	// leaked admin token.
	"/perspectiv.v1.ModerationService/ListAuditLog":   {Access: Admin},
	"/perspectiv.v1.ModerationService/LookupUser":     {Access: Admin, RateLimit: LimitSearch},
	"/perspectiv.v1.ModerationService/LiftSuspension": {Access: Admin},
	"/perspectiv.v1.ModerationService/Restore":        {Access: Admin},
	"/perspectiv.v1.ModerationService/MakeVenue":      {Access: Admin},
	"/perspectiv.v1.ModerationService/MergePlace":     {Access: Admin},
}
