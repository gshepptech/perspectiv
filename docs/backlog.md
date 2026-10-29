# Backlog

What is left before a public beta, in the order it is being worked. An item
moves to Done in the same commit as the work, with what was verified.

## Doing

2. **Legal and trust pages.** Terms, Privacy and Community guidelines as
   public pages, linked from the footer and from sign-up.

## Next

3. **Download my data.** A settings action that returns the account's own
   data (profile, stories, characters, claims, thread posts, follows) as JSON.
4. **Review findings.** Fix what the security and accessibility reviews of
   the post-build features confirm.
5. **Hour and day listings.** A place page lists an hour arc ("Dryer number
   six · 23:00") apart from its day; group an hour under its day.
6. **Installable on phones.** A web app manifest and icons, so "Add to home
   screen" gives a real app icon and name.
7. **Notification digest.** An optional daily email instead of one email per
   event, for people who follow busy places.

## Waiting on a decision or an event

- **History before any push.** Two early commits on this branch hold 58 MB of
  stray binaries; squash them before the branch is pushed. Rewriting history
  waits for the owner's go-ahead.
- **Intermittent race in TestServeByRole.** Seen twice under the full gate,
  never in eight reruns. `make test-integration` now keeps
  tmp/test-results/integration.json with the race report for the next one.

## Done

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
