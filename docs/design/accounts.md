# Folio: accounts and admin

How profiles, the inbox, settings, sign-in, following, saving and the admin
pages look. This page is part of [Folio](../design-system.md); the principles,
tokens, components and voice there apply to everything here.

## Profile

A profile reads top to bottom: the header (avatar, username, joined date,
canon and lore badges), the Follow row, the bio, the track record, then the
writer's work in two tabs.

- **Track record.** One row of four counts under the bio (under the Follow
  row when there is no bio), in this order: stories, upvotes, linked
  writers, places. Each count is the number in bold `ink` (bold being for
  counts that need to be scanned) with its words in `ink-muted`, at
  `text-sm`, as one list item, so a screen reader reads "12 stories", "340
  upvotes", "Linked with 5 writers", "3 places" ("1 story", "1 upvote",
  "Linked with 1 writer", "1 place" for one). The linked writers' words
  sit either side of the number, which stays the bold figure. The list is named "Track record". A zero
  shows as "0 stories" and so on. The row wraps (`flex-wrap`, 24px between
  counts, 8px between lines) and never scrolls sideways at 375px. It is
  plain text: nothing in it is a link or a button. It counts only what a
  reader can open: published stories that are not deleted, hidden or filed
  at or below a hidden place; every upvote on those stories, so the total
  is the sum their story pages show;
  the distinct live people linked to those stories by an accepted claim
  either way, their story readable too; and the distinct places those
  stories sit at. The badges stay in the header.
- **Tabs.** Under the track record, Radix Tabs named "wren's writing" with
  two tabs, "Stories" (the default) and "Thread posts", 44px tall with the
  accent underline on the current one. The current tab lives in the URL:
  Thread posts is `?tab=posts`, Stories has no parameter, and any other
  value reads as Stories. Switching replaces the URL rather than adding a
  history entry, moves no focus, and keeps the reader's place on the page.
  Each panel loads its list the first time it is shown. The tab row's
  hairline is the only rule above the first row: a list under the tabs
  draws its dividers and a closing rule, never a top border of its own.
- **Badges** in the header are flairs (Flairs and tags): "In 2 canons"
  with a `Crown` in `accent`, "In 5 lores" with `Users` in `ink-muted`.
- **Stories tab.** Each story row is one link at least 44px tall. It
  leads with where and when (place and day, `ink`, medium weight), then the
  day's title in the serif face, then the story's own opening (its first
  part, clamped to two lines), then its standing in `text-sm`, so a
  writer's stories are told apart even when their days share a title.
  "Show more" brings 20 more and moves focus to the first new row. With
  none, it says "wren has not published a story yet."
- **Thread posts tab.** The writer's posts in lore threads, newest first,
  replies included, as a list named "Thread posts". Each row is one link
  at least 44px tall to the lore's page: first the lore's name ("Unnamed
  lore" in italics while its members have not named it), medium weight in
  `accent`, underlined on hover, as a story row's title is; then the day's title, the place and the day in `text-sm`
  `ink-muted` ("Saturday night · Prairie Books · January 14, 2026"); then
  the post's text in the interface face, its line breaks folded, cut at a
  word near 240 characters with "…" and clamped to three lines; then a
  `text-sm` `ink-muted` line with "Reply" for a reply and the time it was
  posted. Only posts a reader of the lore page would see are listed: never
  a deleted or hidden post, a reply under a hidden post, or a post in a
  lore whose day sits at or below a hidden place. "Show more" brings 20
  more and moves focus to the first new row. With none, it says "wren has
  not posted in a lore thread yet."

## Inbox and requests

- **Inbox rows.** A notification with a page is one link the full width of
  its row, at least 44px tall: the "New" tag, the sentence and the time
  all sit inside it, so a press anywhere on the row opens it. Only the
  sentence underlines on hover. Following the link, by any click or by
  Enter, marks the notification read. A notification with no page is plain
  text and has no hover.
- **Who might be in a new story.** A "new story on your day" row
  adds a `text-sm` `ink-muted` line under its sentence, inside the link:
  "Who might be in it: “the woman with the foam parrot”, “the clerk” and
  2 more." It names up to three of the story's characters nobody has
  claimed yet, in the story's order, then "and N more". The line is left
  out when every character is claimed, and when the story can no longer be
  claimed from: deleted, hidden, voted out of its lore, at a hidden place,
  or by a deleted account. The email for the same event ends its paragraph
  with the same sentence, read when the email is sent, then "If one of
  them was you, open the story and claim them." Each label is one line.
- **Judging a request.** An incoming request card quotes the claimant's
  own story under the "*claimant* says they are “…”" line: about 240
  characters of its opening, cut at a word and ending "…", in Literata at
  the body size, `ink-muted`, set off by a 2px `rule` left border. A "Read
  their story" link (44px tall) follows it. When their story can no
  longer be read (deleted, hidden, or at a hidden place) both are left
  out and the card says "Their story can no longer be read."
- **Requests** (`/claims`, headed "Requests" like its navigation item)
  keeps its two tabs. Once the reader accepts a request, the bare
  "Accepted" gives way to a `status` line in `success` with a check:
  "Linked. You are both in this story's lore now: read it" (the check
  stays on the first line beside "Linked." and the sentence wraps beside
  it at 375px), linking to the
  lore (or the day when the lore is not known yet), then a `text-sm`
  hint: "A lore needs 4 members to compete for canon." with an "Invite
  who else was there" link to the reader's own story, where the invite
  dialog opens. The invite dialog asks only about characters nobody has
  claimed yet.
- **What a request is about.** Each request names the story it concerns,
  as a `text-sm` `accent` underlined link 44px tall, never a bare "Your
  story": "About your story at UCF Stadium, October 3, 2026" on a
  request on the reader's characters, "About otter's story at UCF Stadium,
  October 3, 2026" on one of the reader's own claims, the day as the
  reader sees days elsewhere ("October 3, 2026, 21:00" for an hour's
  story). On one of the reader's own claims a second link, "Your story of
  that day", opens the story they claimed from.
- **Are you in their story too?** Once the reader accepts, the Linked lines
  are followed by the claimant's characters that are still open to the
  reader: no pending or accepted claim on them, and none the reader made
  before. A `text-sm` `ink-muted` line "Are you in otter's story too?"
  heads a list, one row per character: its description in quotes in
  Literata, then a "That was me" link (44px tall) to
  `/perspectives/<their story>?claim=<character id>`, where the claim
  dialog opens. The block is left out while their story loads, when it
  cannot be read, and when no character is open.
- **Focus after a decision.** Accept and Reject leave with the request
  they decide, so focus moves to the request's new status line ("Linked…"
  or "Rejected"), never to the page body.
- **Waiting for a link.** Where a story's byline says "Waiting for a
  link", a `text-sm` `ink-muted` sentence under the byline says what it
  means: it joins a lore when someone it describes claims their
  character, or its author claims a character in another story that day.

## Settings: signed-in devices

Each device is a row: its browser and system, then "This device" in
`success` for the current one, set apart by a space so it never reads
"macOSThis device"; under it when it signed in and was last used. Every
other row has its own "Sign out" whose accessible name adds the device and
when it signed in ("Sign out Chrome on macOS, signed in Oct 3, 2026"), so
two rows for the same browser are told apart.

## Settings: two-factor sign-in

A block of "Sign-in and security", after "Change email" and before
"Signed-in devices", with the `h3` "Two-factor sign-in" and the anchor
`#two-factor` (the admin notice links to it). The copy calls it
"two-factor sign-in" and the app "your authenticator app"; it never says
TOTP, OTP, 2FA or MFA.

- **Off.** One line, "Off. With it on, signing in asks for a code from an
  authenticator app on your phone as well as your password.", and a
  `secondary` button "Turn on two-factor sign-in…". It opens the steps in
  place, below the line, not in a dialog: the steps are long, a QR code in
  a dialog is cramped on a phone, and the reader may switch to their phone
  and back.
- **Step 1, password.** "Current password" (the same field and refusal
  wording as Change password) and a primary "Continue". Cancel returns to
  Off.
- **Step 2, add to your app.** The line "Add Perspectiv to your
  authenticator app" takes focus, then a numbered list of two items, so
  the order is read out:
  1. "Scan this code with your authenticator app." The QR code sits on a
     `paper` square with a 16px quiet zone and a 1px `rule` border, 192px
     wide on every screen, drawn as SVG in the page by `qrcode.react` from
     the `otpauth://` URI the server returned; nothing is fetched to draw
     it. Its modules are `ink` (`currentColor`) on the square's `paper`,
     and the square sets `color-scheme: light`, so in the dark theme too
     the code stays dark on light, as scanners expect. It is an `img` named "QR code for adding Perspectiv to your
     authenticator app". Under it, "Can't scan it? Enter this key
     instead:" and the key in `font-mono`, grouped in fours with spaces,
     selectable, with a `ghost` "Copy key" button (status line "Key
     copied."). The key is the whole secret, so it is shown only here.
  2. "Enter the six-digit code the app shows." A field labelled "Code from
     your app", `inputMode="numeric"`, `autocomplete="one-time-code"`,
     six characters wide, then primary "Turn on". A wrong code says "That
     code is not right. Check that your phone's clock is set
     automatically." under the field. The fifth wrong code ends the setup
     (the server deletes the new key; these wrong codes never count toward
     the sign-in lockout), and the step shows the `FormAlert` it shows for
     an expired setup, "This setup has expired. Cancel and start again."
     Turning it on signs out every other device, as changing the password
     does, and the "Two-factor sign-in is on" email says so.
- **Step 3, recovery codes.** Shown once, right after it turns on, with
  focus moved to its heading "Save your recovery codes". The line under it:
  "Each code signs you in once if you lose your phone. This is the only
  time we show them." The ten codes are a two-column list (one column under
  400px) in `font-mono`, `text-base`, each code a list item. Below them,
  `secondary` "Download as a text file" (saves
  `perspectiv-recovery-codes.txt`: a heading line naming the account, the
  date, then one code a line) and `ghost` "Copy codes". Then the primary
  "I've saved my codes", which closes the step and shows On. Leaving the
  page also closes it; the codes cannot be shown again, only replaced.
- **On.** A `success` `ShieldCheck` icon and "On since Oct 3, 2026.", then
  "8 recovery codes left." (with 3 or fewer the count is in `warning` and
  adds "Make new ones soon."; at 0 it reads "No recovery codes left. Make
  new ones."). Two
  `secondary` buttons: "Make new recovery codes…" and "Turn off…". Each
  opens a `Dialog` asking for "Current password" and "Code from your app or
  a recovery code", with the description saying what happens ("Your old
  codes stop working." / "Signing in will ask only for your password.").
  A wrong password is said under the password field, a wrong code under
  the code field. Making new codes then shows Step 3 with the new set.
  The server emails the member "Your recovery codes were replaced" when
  codes are made, and "A recovery code was used to…" whenever a recovery
  code makes new codes or turns two-factor sign-in off, as it does for a
  sign-in.
- **Lost your phone and your codes?** A closing `text-sm` `ink-muted`
  paragraph on both Off and On: "Lost your phone and every recovery code?
  Write to us from this account's email address. We check it is you
  before we turn two-factor sign-in off, and we email you when we do."
  The operator path behind it is in `deploy/k3s/README.md`.

## Signing in: the second step

When the password is right and the account has two-factor sign-in on, the
sign-in page stays at `/login` and swaps the form for a second step; the
address does not change and nothing is stored in the browser but the
challenge, held in component state. Focus moves to the new heading.

- Heading "Enter your code", then "Open your authenticator app and enter
  the six-digit code for Perspectiv." One field, "Code from your app"
  (numeric, `one-time-code`, so a phone offers the code), and primary
  "Sign in".
- Under the button, a text button "Use a recovery code" swaps the field
  for "Recovery code" (`autocomplete="off"`, `font-mono`, help line "Like
  abcd-efgh-jkmn-pqrs. Each code works once.", the line above it reading
  "Enter one of the recovery codes you saved when you turned on two-factor
  sign-in.") and itself for "Use the code from your app", keeping focus.
  Both buttons are 44px tall.
- "Start again" (a text button) returns to the email and password, which
  keep the email.
- A wrong code: "That code is not right." under the field, and the field
  keeps focus. Too many: the `FormAlert` "Too many wrong codes. Two-factor
  sign-in for this account is paused; try again later." A challenge that
  timed out (about 5 minutes) or was ended by a password reset returns to
  the password form with the `status` line "That sign-in timed out. Enter
  your password again."
- After a recovery code, the page the reader goes to is the one they were
  headed for, and Settings shows how many codes are left; the email the
  server sends says so too. Nothing blocks the way in.

## Admins without two-factor sign-in

v1 does not require admins to turn two-factor sign-in on, and there is no
lockout or deadline: an admin without it keeps every admin page. It is
asked for instead:

- **The notice.** While the signed-in account is an admin without
  two-factor sign-in, a band sits under the header on every page: full
  shell width, `surface` fill, a 1px `rule` bottom border, a 16px
  `warning` `ShieldAlert` icon, then "Your admin account has no two-factor
  sign-in." and the link "Turn it on in Settings" (to
  `/settings#two-factor`). It is a `region` named "Account security
  notice", not an alert: it is there on load, not news. It has no close
  button; it goes when two-factor sign-in is on, read from the session the
  server returns, and comes back if it is turned off.
- **The dossier** adds a fact "Two-factor sign-in", "On" or "Off", for
  every account, after "Failed sign-ins, last 24 hours". For an admin
  without it the word is "Off" in `warning`.
- **The audit log** names an operator's reset from the command line as
  "**The command line** reset a user's two-factor sign-in: *username*",
  with the reason quoted below like any entry, and lists "Reset a user's
  two-factor sign-in" in the action filter. The reason begins with the
  name of the operator who ran it ("operator Dana Ops: owner confirmed by
  email…"), which the command requires.

## Following writers

- **Profile toggle.** Another member's profile carries a "Follow" toggle
  under the username, the same button as the place page's: ghost with a
  bell when off, `secondary` with a crossed bell reading "Following" when
  on, `aria-pressed` either way, and its accessible name adds the writer
  ("Follow wren"). A signed-out reader who presses it is sent to sign in.
  Beside it a `text-sm` `ink-muted` line counts followers ("1 follower",
  "12 followers"), a polite live region so the new count is read after a
  press; the count shows to everyone, the toggle only on someone
  else's live profile, never on your own or a deleted one. A refusal (a
  block either way) is a `role="alert"` line in `error` under the toggle.
- **Settings: Writers you follow.** A section headed "Writers you follow"
  sits just above "Blocked users": one line of help ("A new story by a
  writer you follow lands in your inbox."), then one row per writer, newest
  follow first, each row the avatar (32px, round, only when set) and the
  username as a link to their profile, and an "Unfollow" `secondary` `sm`
  button on the right whose accessible name adds the writer ("Unfollow
  wren"). Unfollowing removes the row once the server confirms, and a
  `status` line under the list, "You no longer follow wren.", takes focus,
  since the button that had it is gone. With none, the section says "You
  do not follow any writers yet." The username link and the Unfollow
  button are each at least 44px tall, and long usernames wrap at 375px.
- **Inbox.** A followed writer's story reads "*writer* wrote a new story in
  *day*." and opens the story; its settings toggle is "A new story by a
  writer you follow".

## Saving stories

- **Save toggle.** Every readable story's action bar carries a "Save"
  toggle in its second row, before Report, built like the Follow toggle:
  `sm`, at least 44px tall, `ghost` with an outline lucide `Bookmark`
  reading "Save" when off, `secondary` with a filled `Bookmark` reading
  "Saved" when on, `aria-pressed` either way, and an accessible name that
  adds the story ("Save otter's story", "Saved otter's story")
  so a page of cards reads as distinct buttons. Like Report it is not
  offered on the reader's own story, and never on a deleted or hidden
  placeholder. A
  signed-out reader who presses it is sent to sign in and comes back to the
  story, as Report does. A refusal is a `role="alert"` line in `error`
  under the bar.
- **Saved page.** `/saved` (signed in only) is headed, and titled, "Saved
  stories", with one `ink-muted` line under the heading: "Stories you saved
  to read later, most recently saved first." Its rows are the profile's
  rows (one link at least 44px tall: where and when, the day's title in the
  serif face, the opening clamped to two lines, the standing), except that
  the first line ends with the writer ("Joe's Diner · Oct 15 · by otter"),
  since one list holds many writers. "Show more" brings 20 more and moves
  focus to the first new row. A saved story that is deleted or hidden
  leaves the list without a word and comes back if it is restored. With
  none, the page says "You have not saved any stories yet. Press Save under
  a story to keep it here."
- **Unsaving from the list.** Each row carries the story's Save toggle
  under its link, `flush`, reading "Saved". Pressing it unsaves the story,
  but the row stays until the reader leaves the page: a `text-sm`
  `ink-muted` "Removed." appears before the toggle (a visually hidden
  polite `status` after it says the same to a screen reader), and the
  toggle reads "Undo", named "Undo: save otter's story again", with
  no `aria-pressed`. Undo saves it again in place. Focus stays on the
  button throughout, since it is the same button. The next visit lists
  only what is saved.
- **Way in.** The account menu lists "Your profile" (lucide `User`,
  linking to `/users/<username>`) first, then "Saved" with a `Bookmark`
  icon, above Settings.

## Appearance

- `/appearance` is open to everyone, signed in or not, and linked from the
  footer ("Appearance") and from Settings. Its choices belong to the device,
  not the account: they are kept in `localStorage` (read and written inside
  try/catch, so a blocked store only means the defaults) and never sent to
  the server.
- **Theme**: a radio group, "Match this device" (the default), "Paper
  (light)" and "Lamplight (dark)".
- **Story text size**: a radio group, "Standard" (18px), "Large" (20px) and
  "Larger" (22px). It sizes story text (`prose-body`) only; the interface
  keeps its size, and the measure stays 65 characters.
- A choice applies at once, with no save button, and a `text-sm` line under
  the groups says "Saved on this device." It is a polite `status` that is
  read again after every choice, not only the first. Every option is a
  native radio with a visible label, at least 44px tall. The page heading
  takes focus on arrival and shows the focus outline like any other.
- The choice is applied before the first paint by `public/appearance.js`,
  which sets `data-theme` and `data-text` on the root element and the page's
  `theme-color`, so a page never flashes the other theme.

## Sign-up during the beta

While sign-up is invite-only (the server says so through GetSignUpInfo):

- The sign-up form gains an **Invite code** field first, filled from the
  link's `?code=` and still editable, with the help line "From a member's
  invite link or a beta invite."
- Under the form, a quiet line: "No invite? Join the waitlist." It reveals a
  short form in place (email, and an optional "Where would you write about?"
  of up to 300 characters). On success the form is replaced by a `status`
  line: "You're on the list. We'll email you when there's a place." It says
  the same whether or not the address was already listed.
- The invite page offers a signed-out visitor "New here? Sign up with this
  invite", carrying the invite's code to the sign-up form.
- While sign-up is open none of this shows.
- **Keeping the journey.** A reader sent to sign in from a page keeps that
  page through "Create an account", sign-up and email verification: the
  verified page offers "Continue where you were" (through sign-in when the
  tab is signed out). The password field says its rule before submit: "At
  least 10 characters; a common password is refused."
- **The verified page is a start, not a dead end.** Verifying is when a
  new member may first publish, so with nowhere kept to continue to the
  page offers two text links on one wrapping line, each 44px tall: "Write
  about a place you were at" first, opening the composer's place picker
  (`/compose`), then "Start reading" (`/`). When the tab is signed out they
  read "Sign in to write about a place you were at" (sign-in, then
  `/compose`) and "Sign in". With a kept path, "Continue where you were"
  is the only link, as before.

## Admin: beta access

`/admin/access`, reached from the account menu ("Beta access") for admins.
Two tabs, **Codes** and **Waitlist**, built on the report queue's patterns:

- **Codes:** a create form (code, blank to generate; note; uses, default 1;
  expires in days, blank for never) above a list, newest first. Each code
  shows its uses ("3 of 25 used"), note, expiry and standing (Live, Used up,
  Expired, Revoked; words, not colour alone), a "Copy invite link" button
  that copies `/signup?code=…`, and "Revoke" for live codes.
- **Waitlist:** people waiting first, each with a masked address, note and
  date, and "Send invite". Invited people show when and with which code.

## Admin: audit log and user lookup

Two more admin pages in the account menu, after "Beta access": **Look up a
user** and **Audit log**. Both follow the report queue's patterns: a page
heading, a plain list of entries with `rule` dividers, "Show more" to page.

- **Look up a user** (`/admin/users`): one field labelled "Username, email
  or account id" and a "Look up" button. A lookup runs only on submit, never
  as the admin types, because each match is itself audited. The query sits
  in the address (`?q=`), so a dossier is linkable from the queue and the
  log. The dossier below the form:
  - the username as an `h2`, then standing in words beside it: Admin,
    Suspended (with the date), Deleted, Email verified or Email not
    verified. Status colours carry their fixed meaning (`warning` for
    suspended, `error` never, since a deleted account is not an error).
  - a two-column `dl` (one column on phones): email (masked, as
    `pa•••@example.test`), joined, last sign-in or "Never", failed sign-ins
    in the last day, stories (and how many are hidden), reports against
    (open of total), reports filed. Then "Public profile" unless deleted.
  - **Reports against them**: the queue's report items for this user (on
    the account or anything they wrote), open and resolved together, with
    "Resolve" on open ones.
  - **Moderator actions**: the audit entries about this user.
- **Audit log** (`/admin/audit`): an action filter (a Select: "All
  actions" and each action) above the entries, newest first. Each entry
  reads as one sentence then detail: "**ana** hid a story: *Rain at
  the deli*", with the time below in `ink-muted` and the reason, when
  there is one, quoted. An entry from the command line names "The command
  line" as its actor. A target that has a page links to it; a user target
  links to its dossier.
- In the report queue, the reporter's name links to their dossier.
- A report shows what its target said when it was filed, so an edit after
  the report cannot hide it: a `<details>` disclosure, "What it said when
  reported", closed by default, its summary 44px tall with a 16px
  `ChevronRight` that points down while it is open, holding the text in
  `whitespace-pre-wrap` with the serif face for story and post text, at
  most 20 lines tall (`20lh`) with its own scroll. The scrolling box is in
  the tab order, named "What it said when reported", so a keyboard can
  scroll it. A report's "Resolve" and its link to the target are 44px
  tall.
- **Undoing a decision.** A suspended account's dossier offers "Lift
  suspension…" beside its standing; a hide entry in the audit log
  (story, place or thread post) offers "Restore…". Both open the
  same small dialog: what is being undone, a required "Reason for the
  audit log", and a primary button naming the action. On success the
  dialog closes, a status line says what changed and takes focus, and the
  dossier or log re-reads, so the new entry appears at the top. A lift's
  line sits under the standing, where the button was; a resolve's line
  stays above the reports. A restore
  of something already visible says so in the dialog instead.
