# Backlog

What is left before a public beta, in the order it is being worked. An item
moves to Done in the same commit as the work, with what was verified.

## Doing (wave 4)

15. **Query performance, part 2.** The structural fixes from the beta-scale
    audit: lore groups computed in Go from claim edges (ArcGroups, 3.7 s on a
    large group, under the arc lock), a `places.path` array so a search
    scoped to a state stops walking 509k descendants (1.6 s), a readable
    story count on arcs for ListStoriedChildren, and a capped label-first
    plan for SearchCharacters (570 ms for a rare word in a big city).

## Out of scope for v1

- Full-text search over stories: the spec leaves it out (A-057); place
  search and character search are the ways in.

## Waiting on a decision or an event

- **Legal review and a contact address.** The Terms, Privacy and
  Community guidelines pages describe what the code does today; they need a
  lawyer's review and a real contact address from the owner before launch.

## Done

- Query performance, part 1, from a plan audit at beta scale (225k
  stories, 638k places, 27k followers of Florida): the new-story fan-out
  to place followers uses NOT EXISTS (112 s to 79 ms); thread posts check
  membership by the lore argument (300 ms to 3 ms); an arc's tales and
  unlinked stories plan as anti-joins (150 ms to 8 ms); partial indexes for
  hidden places, recent tales and due digests; one- and two-letter place
  searches match name prefixes on a pattern index instead of scanning; and
  the pool forces per-call plans, since a generic plan of a place listing
  ran 550 ms against 12 ms. Verified by the domain integration suites and
  migration 21 up-down-up.
- Data retention: a daily job deletes, in batches, emailed links 7 days
  after use or expiry, sessions and their refresh tokens 30 days after they
  end, sign-in attempts after 30 days and rate-limit counters after a day,
  with indexes for each cutoff; the privacy page states the periods.
  Verified by TestRetentionPurge and migration 20 up-down-up.
- TestServeByRole flake, diagnosed: the API role hashed a bcrypt dummy
  password at cost 12 while building the auth service, before it listened.
  That takes 0.3s, 3s under the race detector and over 30s under the full
  gate's load, so the health wait gave up. A goroutine dump of a stalled
  run showed both API servers inside bcrypt; Postgres held no waiting
  locks. The dummy hash is now a precomputed cost-12 constant (a throwaway
  secret), computed only at a non-default cost. Verified by
  TestDummyHashMatchesCost and the full gate.
- Moderation tools: an audit log at /admin/audit (newest first, filter by
  action, each entry a sentence linking its target) and a user lookup at
  /admin/users (by username, email or id) with a dossier: masked email,
  standing, sign-ins, stories, reports against and filed, the reports
  themselves and the moderator actions about the account. The audit log
  now records access code creation and revocation, waitlist invites and
  each user lookup, in the same transaction as the action, under its own
  AuditAction and AuditTargetKind enums. The report queue filters by the
  user reported and by the reporter, and links reporters to their dossier.
  Migration 014's Down now runs past the append-only trigger. Verified by
  TestAuditLog, TestAuditLogIsAdminOnly, TestLookupUser,
  TestListReportsFilters, TestWriteAuditRefusesMalformedEntries, the beta
  tests' audit assertions, migration 19 up-down-up, and the page tests.
- Email unsubscribe: every notification and digest email carries an RFC
  8058 one-click List-Unsubscribe header and a footer link to /unsubscribe,
  signed per account (HMAC from the server secret), which turns every
  notification email off and leaves in-app notifications as they were.
  Account emails carry neither. Opening the page changes nothing until
  "Stop emails" is pressed; a GET to the one-click endpoint is refused.
  Verified by TestUnsubscribeLink, TestOneClickUnsubscribe,
  TestNotificationEmailsCanBeUnsubscribed, TestBuildMIMEUnsubscribeHeaders
  and the page's unit tests.
- Account security review fixes: a pending email change is cancelled by a
  password reset, a password change and signing out everywhere, and
  confirming a change spends every reset and verification link sent to the
  old address. Refusals are logged by reason, not as wrong passwords.
  Verified by TestEmailChangeCannotTakeOver.
- Mobile pass: twelve screens measured at 375x812. Nothing overflowed; the
  defects were tap targets. Sign up, Write your perspective, Read yours,
  story-row Read, byline links, Upvote, reacts (8px apart), Report, the
  login links, place arc titles and both "That was me" links are now 44px;
  a thread post's Report stays on its header row; the landing placeholder
  fits the field; an hour arc's page says its hour once. Verified by
  measuring the live loop at 375px, the unit suite and the e2e suite.
- Account security: Settings has "Sign-in and security". Changing the
  password takes the current one (counted by the login lockout), retires
  reset links and signs out every other device. Changing email takes the
  password, mails a 48-hour link to the new address and a notice to the old
  one, and changes nothing until the link is followed; a newer request
  replaces an older one, and an address taken in the meantime is refused.
  "Sign out everywhere" revokes every session. Verified by TestChangePassword,
  TestChangePasswordHonoursLockout, TestEmailChange, TestEmailChangeRaces,
  TestSignOutEverywhere, the template tests and the settings and
  /confirm-email unit tests.
- A livelier beta: five more seed days (Tampa, Key West, Jacksonville,
  Saint Petersburg, Sarasota) with 26 new writers, three canon lores, two
  contenders, four tales, named lores and thread posts. Characters are
  described, never named, and no place is a business. Verified by
  `make seed` on the dev database and the rank pass promoting the canons.
- Notification digest: "Send my emails as one daily summary" in Settings.
  While on, each email-worthy event becomes a digest item; an hourly job
  sends each digest account one summary of its items at most once a day
  (one line and link per event) and clears them. Verified by TestDigest,
  TestDigestSetting, the digest render test and the settings test.
- Installable on phones: an app icon (the book mark on ink blue) as SVG,
  Apple touch, 192, 512 and maskable PNGs, a web app manifest served as
  application/manifest+json, and light and dark theme colours. There was no
  favicon at all before. Verified by TestSPA and the built bundle.
- Hour and day listings, decided: an hour arc is where its stories live and
  the day page leads with its hours, so place listings keep listing the hour
  arc. Folding hours into days would rework the ranked and newest keysets
  for little gain. The real defect, the hour said twice ("… · 23:00" and
  "October 11, 2026, 23:00"), is fixed everywhere a title sits beside its
  date (formatDayBeside). Verified by its unit test and the route tests.
- Download my data: ExportMyData (5 an hour) returns one JSON document built
  in Postgres with the account, stories with parts and characters, claims
  both ways, votes, reactions, thread posts, lores named, follows, blocks,
  reports, invites and notification settings, and no other member's email;
  Settings saves it as a file. Verified by TestExportMyData and the settings
  test.
- Legal and trust pages: /terms, /privacy and /guidelines, written from
  what the code stores and sends (email provider only, one sign-in cookie,
  no trackers), linked from the footer and the sign-up terms. Verified by
  the route contract and keyboard-order tests.
- Accessibility review fixes: a pending button keeps focus (aria-disabled,
  not disabled) everywhere; focus moves to a stable place after a claim, a
  revoke, a waitlist invite and a delete; the claim dialog says "Your claim"
  and names each character; claim, search and copy results are announced;
  form errors are tied to their fields; upvote and report buttons name what
  they act on and keep one name when pressed; navigation targets are 44px;
  the phone "Is this you?" shows a disclosure arrow; the post-publish list
  follows the story's title. Verified by unit tests (Button pending, claim
  focus) and the e2e suite.
- Security review fixes: a member's invite link admits at most 5 sign-ups
  and only while its story is readable; thread posts and lore names are rate
  limited (30 an hour); a moderator's name clear locks renaming for 7 days;
  waitlist code notes carry masked addresses; admin denials log method and
  reason; description search is rate limited and has a trigram index.
  Verified by TestMemberInviteLimits, TestNameLore, TestResolveActions,
  TestWaitlistInvite and the policy tests.
- Sign-up abuse, offline: a honeypot field on sign-up and the waitlist (a
  filled one is answered as a success and stores nothing), and 2,311 common
  passwords of 10+ characters from SecLists (MIT) refused by sign-up and
  reset, ignoring case. Verified by TestHoneypot, TestValidatePassword and the
  sign-up unit tests. Third-party CAPTCHA and breach APIs stay out: runtime
  calls are limited to the email provider.
- Invite-only beta: access codes, member invite links, waitlist, admin page
  (87cc0ed, b385107). Verified by integration tests, unit tests and e2e.
- "Is this you?" on arc pages, place description search, post-publish prompt
  (d8427b4, 5e72283, b00ce4d, 6587797). Verified by e2e find-yourself.
- Link previews, report thread posts and lore names, arc page vote reads,
  follow a place, lore names, front-page places (7c5424c and earlier).
