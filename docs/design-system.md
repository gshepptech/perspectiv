# Folio

Folio is the design system for Perspectiv. Perspectiv reads like a novel, not a
feed: a perspective is a short book, each part is a page, and the chrome around
it stays quiet. This file is the output of the design pass. The project
`CLAUDE.md` names it, and where it and component code disagree, this file wins.

The tokens below live in `web/src/app/styles.css`. Component code uses the
Tailwind utilities those tokens generate (`bg-paper`, `text-ink`,
`font-serif`, `max-w-measure`) and never a raw hex value.

## Principles

1. **The page is the product.** Prose gets the width, the type and the
   contrast; navigation and actions sit at the edges.
2. **One thing per page.** A reading page shows one perspective: a part at a
   time, or whole when it is short or the reader asks for it. There is no
   infinite scroll of stories.
3. **Paper, not glass.** Flat surfaces separated by hairline rules. No
   gradients, no shadows deeper than a 1px border, no blur.
4. **Calm motion.** Nothing on a reading surface moves unless the reader asked
   it to, and reduced motion means none at all.

## Type

| Role | Face | Token | Fallback stack |
|---|---|---|---|
| Prose, titles, place names | Literata (variable, self-hosted) | `--font-serif` | `Georgia, "Times New Roman", serif` |
| Interface: buttons, labels, navigation, metadata | Public Sans (variable, self-hosted) | `--font-sans` | `system-ui, -apple-system, "Segoe UI", sans-serif` |

Literata was drawn for long-form reading on screens and keeps its texture at
phone sizes. Public Sans is a neutral grotesque that stays out of the prose's
way. Both are bundled from `@fontsource-variable/*` packages into the build;
the app MUST NOT load fonts from a third-party host at runtime (the only
runtime third party is the email provider).

Scale (rem at a 16px root):

| Step | Size | Line height | Use |
|---|---|---|---|
| `text-sm` | 0.875 | 1.43 | metadata, helper text, timestamps |
| `text-base` | 1 | 1.5 | interface copy, form fields |
| `text-prose` | 1.125 | 1.7 | reading body (serif) |
| `text-xl` | 1.25 | 1.4 | section headings |
| `text-2xl` | 1.5 | 1.33 | page titles on phone |
| `text-3xl` | 1.875 | 1.2 | page titles from the `sm` breakpoint |

Headings use Literata at weight 600. Interface text uses Public Sans at 400 and
500 for emphasis; bold (700) is reserved for counts that need to be scanned.
Running prose is left-aligned, never justified, with `hyphens: auto`.

## Measure and layout

- **Reading measure:** prose columns are capped at `--measure: 65ch` of the
  serif face (about 62 to 70 characters a line).
- **Interface width:** lists, forms and the arc page are capped at
  `--width-shell: 72rem`.
- **Gutter:** every page has a 16px side gutter (`px-4`) at every width. No
  element sets a width larger than its container, and the shell clips nothing:
  a page that scrolls horizontally at 375px wide is a defect.
- **Shell:** a single-row header (wordmark left, primary navigation and the
  account menu right), then `<main id="main">`, then a one-line footer. A skip
  link to `#main` is the first focusable element.
- **Primary navigation** for a member is Write, Requests, Inbox and the
  account menu, each an icon with a visible word. Below `sm` the word sits
  under its 20px icon in `text-xs` and the navigation may wrap under the
  wordmark; from `sm` the word sits beside the icon. Requests uses
  `UserCheck` and the account menu `CircleUser`, so no two items share an
  icon. The account item reads "Account" on phones and the username from
  `sm`. Unread notifications show as a count badge on the Inbox icon
  (`accent` fill, `accent-ink` text, bold), and the link's accessible name
  carries it ("Inbox, 2 unread"); with none unread there is no badge.
  Requests waiting on the member to accept or reject show the same badge
  on the Requests icon, named "Requests, 2 waiting" ("99+" in the badge
  past 99); with none waiting there is no badge.
- **Breakpoints:** Tailwind defaults. Layout is single column below `md`
  (768px); from `md` the arc and place pages MAY use a two-column layout whose
  reading column keeps the measure.
- **Spacing:** the 4px Tailwind spacing scale. Vertical rhythm between blocks
  of prose is one line (`1.7em`).

## Palette

Two themes, **Paper** (light) and **Lamplight** (dark), follow the
operating system's `prefers-color-scheme` unless the reader picks one on the
Appearance page. Every text pairing meets WCAG 2.1 AA; the
ratios below are computed against `paper` and `surface`, and the unit test in
`web/src/app/tokens.test.ts` recomputes them from the shipped stylesheet.

| Token | Paper | Lamplight | Use | Ratio on paper / surface (light, dark) |
|---|---|---|---|---|
| `paper` | `#F7F5F0` | `#161514` | page background | |
| `surface` | `#EFECE5` | `#201E1C` | panels, menus, dialogs, inputs | |
| `ink` | `#1F1D1A` | `#E8E4DC` | body text, headings | 15.4 / 14.3, 14.4 / 13.1 |
| `ink-muted` | `#5C574F` | `#ABA59A` | metadata, helper text | 6.6 / 6.1, 7.5 / 6.8 |
| `rule` | `#D6D1C7` | `#34312D` | decorative hairlines only | n/a (non-informational) |
| `control` | `#8A8478` | `#7A746A` | input and control borders | 3.4 / 3.2, 3.9 / 3.6 (3:1 for UI components) |
| `accent` | `#2B4C7E` | `#9DB8E6` | links, primary buttons, focus ring, current page | 7.9 / 7.3, 9.1 / 8.3 |
| `accent-ink` | `#FFFFFF` | `#12223A` | text on `accent` | 8.6, 7.9 |
| `success` | `#2F6B3A` | `#7CC48A` | accepted claims, saved | 5.9 / 5.4, 8.8 / 8.0 |
| `warning` | `#8A5A00` | `#E0B455` | pending, expiring drafts | 5.4 / 5.0, 9.4 / 8.6 |
| `error` | `#A3322A` | `#F08A80` | validation and request errors | 6.3 / 5.8, 7.5 / 6.9 |
| `info` | `#1F5F7A` | `#7FC4DD` | neutral notices | 6.5 / 6.0, 9.4 / 8.6 |

The accent is an ink blue, the colour of a fountain-pen annotation. Status
colours keep their meaning everywhere and MUST NOT be used as decoration or as a
second accent. Status is never carried by colour alone: it always has a word or
an icon beside it.

## Pagination (reading)

- A perspective of up to 10 parts renders **one part per page** as semantic
  HTML: an `<article>` with the perspective title as its heading and the part as
  paragraphs. No canvas, no page-flip, no curl, no slide.
- Each page has **previous** and **next** controls as real `<button>` or `<a>`
  elements, labelled ("Previous part", "Next part"), disabled at the ends, plus a
  "Part 3 of 7" position label announced to screen readers (`aria-live="polite"`).
- **Arrow keys page**: `ArrowLeft` goes back and `ArrowRight` goes forward while
  focus is not inside a text field. The part index is in the URL so a page is
  linkable and the browser back button returns to the previous part.
- After a page change focus moves to the part's heading so a screen reader
  starts reading the new part.
- **Whole story on one page.** A story under 600 words in all shows every
  part on one page, each under its own "Part N" heading (none for a one-part
  story), with no paging controls. A longer story offers a toggle button under
  the byline, "Read all parts on one page" (`aria-pressed`, 44px tall). The
  choice is kept per viewer in `localStorage`, read and written inside
  try/catch, so a blocked store only means paging. Paging keeps `?part=` and
  the arrow keys; on one page the arrow keys do nothing, and an address with
  `?part=N` moves focus to part N's heading.
- **More from this day.** Once the reader is on the last part, or reading the
  whole story on one page, a section follows the story and its actions:
  "More from this day" ("More from this hour" on an hour arc). It holds the
  story's lore when it is in one (a link to the lore, its stories and its
  thread), up to five other readable stories of the same arc as rows (the
  author on a `text-sm` line, the opening in the serif face clamped to two
  lines, the whole row one link at least 44px tall), and a link back to the
  day. It reads the arc's first pages from `GetArc` and nothing else.

## Story page

- **Standing in plain words.** The byline names the story's standing: "In a
  lore", "Canon of this day" ("Canon of this hour" on an hour arc) when its
  lore is the arc's canon, "Waiting for a link", "A tale (no linked stories)"
  or "A tale (voted out of its lore)". A story in a lore links to it ("In a
  lore: read the lore"). One `text-sm` `ink-muted` line under the byline says
  what the standing means and ends with a "How this day works" link to the arc
  page (`/arcs/:id#arc-how`), which scrolls to that section and moves focus to
  its heading.
- **Cast tags open the claimant's story.** An accepted claim's tag links to
  the claimant's own story of that day while it is readable (link named
  "otter's story"); otherwise it links to their profile.

## Page titles

Every route sets `document.title` through one hook, `useDocumentTitle` in
`web/src/lib/title.ts`, as "<what the page shows> · Perspectiv": a story is
"<arc title> by <author>", an arc its title, a place its name, a lore its
name or its day's title, a profile the username, and a fixed page its heading
("Settings", "Inbox", "Requests", "Sign in"). Leaving a page puts back
"Perspectiv", which the front page keeps.

## Motion

- The only transition on a reading surface is a 150ms `opacity` cross-fade
  between parts. Interface transitions (menus, dialogs, tooltips) animate
  `opacity` and `transform` only, under 200ms, and never block input.
- Under `@media (prefers-reduced-motion: reduce)` every transition and
  animation duration is set to zero globally in `styles.css`; components MUST NOT
  override it.

## Interaction

- **Focus:** every focusable element shows a 2px `accent` outline with a 2px
  offset on `:focus-visible`. Outlines are never removed.
- **Targets:** buttons and links in navigation are at least 44px tall on touch
  layouts, and so are the action buttons and links in lists and dialogs
  (Accept, Reject, Claim a character, That was me, the inbox's All, Unread
  and Mark all read, a request's links to its two perspectives).
- **Status lines** that take focus after an action carry `tabIndex={-1}` and
  no `aria-label`, so focusing one reads its message. When an action removes
  the control that had focus (a sent claim or report), focus moves to its
  status line.
- **Hover** styling appears only on elements that respond to a click.
- **Selection** (current tab, current part, active filter) is shown by an
  underline or border in `accent` plus `aria-selected` or `aria-current`, never
  by opacity alone.
- **Keyboard:** every reading and writing action is reachable by Tab and
  operable by Enter or Space; dialogs trap focus and close on Escape.

## Components

The primitives live in `web/src/components/ui/` and wrap Radix so keyboard and
ARIA behaviour come from Radix, not from hand-written handlers:

| Component | Built on | Notes |
|---|---|---|
| `Button` | native `<button>` (Radix `Slot` for `asChild`) | variants `primary`, `secondary`, `ghost`, `danger`; sizes `sm`, `md` |
| `Input`, `Textarea` | native elements | `control` border, `surface` fill, `aria-invalid` turns the border `error` |
| `Dialog` | `@radix-ui/react-dialog` | `surface` panel, full width minus the gutter on phones |
| `Tabs` | `@radix-ui/react-tabs` | the epic's Ranked, Newest and Tales tabs |
| `Select` | `@radix-ui/react-select` | genre, report reason |
| `Tooltip` | `@radix-ui/react-tooltip` | supplementary only; never the sole label |
| `DropdownMenu` | `@radix-ui/react-dropdown-menu` | account menu, per-perspective actions |
| `Form` | `react-hook-form` with `@radix-ui/react-label` | `FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`; errors render inline under the field |

Icons are `lucide-react`, imported by name, at 16px in interface text and 20px
in navigation, with `aria-hidden` when a text label sits beside them.

## Arc page (the day)

The arc page is an index, like a subreddit's front page: it organises the
day's stories so a reader can see what there is and pick one. Reading happens
on the story's own page, so the arc page MUST NOT carry a story's full action
bar (cast, reacts, claim, report); those live on the story's page and on its
lore page.

- **Header:** the place chain, the arc title, then one metadata line:
  the place-local day and who started it. The primary action ("Write your
  perspective", or "Read yours" for a participant) sits in the header at every
  width.
- **Two columns from `lg`:** a feed column (`minmax(0,1fr)`) and an 18rem
  sidebar. Below `lg` the sidebar follows the feed.
- **Lore block:** each lore is one bordered block with a flair line ("Canon"
  with a crown in `accent`, or "Contender"), a title built from its members
  ("knightsfan_04, paintedknight and 3 others"), its counts, then its stories as
  compact rows, then one link to the lore page ("Open the lore: cast, full
  stories and thread"). The canon block comes first and is marked pinned; a
  contender block carries its progress toward canon. On an hour arc the canon
  is "Canon of this hour", never "of the day", here and on the lore page.
- **How this day works, on phones.** Below `lg` the sidebar falls under every
  story, so one `text-sm` line under the header says it in brief ("The canon
  is the lore of 4 or more linked stories that the people who were here
  favour most.") with a "How this day works" link that scrolls to the full
  section and moves focus to its heading. From `lg` the line is hidden.
- **Story row:** a vote rail on the left (the upvote button and its count),
  then the author, genre and part count on one `text-sm` line, then the opening
  of the story in the serif face clamped to two lines, then "Read". The row is
  the only place on the arc page a reader acts on a story.
- **A day written only in its hours** has no stories of its own: the page
  leads with the hour timeline and says so, instead of an empty canon.
- **Standing alone:** stories in no lore (waiting for a link, or tales) are one
  section of story rows below the lores, each row carrying its standing as a
  flair word.
- **Is this you?** At the top of the sidebar: the people described in the
  day's stories whom nobody has claimed, each quoted in Literata with "in
  otter's story" and a "That was me" link that opens the story with the
  claim dialog up. Six at first, then "Show all". Left out when there are
  none; the viewer's own characters are never listed. Below `lg`, where the
  sidebar falls under every story, it is a closed one-line disclosure under
  the header instead ("Is this you? 2 people nobody has claimed yet").
- **Not yet able to claim or upvote.** On a story's page, a reader with no
  perspective in its arc, or a signed-out reader, still sees "Claim a
  character" (when the story has someone open to claim) and "Upvote".
  Pressing either opens a bordered `surface` note in place under the
  actions instead of a dialog or a silent redirect: "To say this was you,
  write your side of this day first. Your story and theirs link once they
  accept." (for upvote: "Only people who wrote about this day can upvote
  its stories, so the people who were there choose its canon."). For a
  claim, a primary "Write your side" link to the composer for that place and
  day follows; for an upvote only a plain "Were you there? Write your side"
  text link, since most readers were not. Signed out, both show "Sign in"
  and "Create an account" links that return to the story. Arriving from
  "That was me" opens the claim note at once and moves focus to it. After
  publishing from that link the writer lands back on the story with the
  claim dialog up, and above the story a `status` line in a `success`
  border says "Your story is published." with an "Open your story" link.
  Because the dialog hides the page from a screen reader, the dialog opened
  this way starts, under its title, with a `success`-bordered line "Your
  story is published. Now say which character was you." that is part of
  its description; the page line stays for after the dialog closes.
- **"That was me" names the character.** Every "That was me" link carries
  the character it quotes (`?claim=<characterId>`; a bare `?claim=1` still
  opens the dialog with nothing picked). The claim dialog then marks that
  character with an `accent` left border and `aria-current`, and focus
  starts on its "That was me" button. The character survives the detour
  through sign-in or "Write your side". The address drops `claim` once
  the story has read it, so a reload does not reopen the dialog.
- **Reacting signed out.** Pressing a react while signed out opens the same
  bordered note under the actions, "To react to a story, sign in or create
  an account.", with "Sign in" and "Create an account" links that return
  to the story, instead of leaving for the sign-in page.
- **Sidebar:** the four steps of how a day becomes canon, the number of
  stories and lores, who may do what here in one sentence, and "Report place".
  A day arc's hours sit in the feed, after the canon.
- **Flairs** are words in `text-sm` with an icon where one exists; canon uses
  `accent`, pending uses `warning`, never colour alone.

## Lore page: thread

The thread under a lore's stories reads like a well-kept forum: posts
ranked by the people who were there, replies grouped under what they
answer, and nothing deeper than one step of indent.

- **Header row:** the "Thread" heading, then a sort control of two Radix
  Tabs, "Top" (most upvoted first, ties oldest first; the default) and
  "New" (newest first). The tabs are 44px tall with the accent underline on
  the current one; switching keeps the reader's place on the page and
  moves no focus. Replies are always oldest first, whatever the sort.
- **A post** is an `<article>` named by its author ("otter's post"). Its
  byline is one `text-sm` `ink-muted` line: the author link, "In this
  lore" flair for a member, the time, and "· edited" when the author
  changed it (the time of the edit in the `<time>` title). The body follows
  in the interface face, wrapping anywhere, keeping the writer's line breaks.
- **Action row** under the body, every control at least 44px tall and
  wrapping at 375px: the upvote toggle (`ArrowBigUp`, the count beside it,
  `aria-pressed` for the viewer's own vote, named "Upvote otter's post");
  "Reply"; then "Edit" and "Delete" on the viewer's own post, or "Report"
  on anyone else's. Someone who may not vote sees the count without the
  toggle, and never sees the toggle on their own post.
- **Replies** sit under their post in an `<ol>` indented one step
  (`pl-4`, a 2px `rule` left border), oldest first. A reply that answered
  another reply opens with "Replying to @name" in `text-sm` `ink-muted`.
  The first three show; under them a ghost button "Show 4 more replies"
  loads the rest a page at a time and moves focus to the first new reply.
  A reply's own "Reply" answers into the same group.
- **Writing:** "Reply" opens a labelled textarea under the post ("Reply to
  otter"), with "Post reply" and "Cancel"; focus moves into it, Escape or
  Cancel closes it and returns focus to the "Reply" button. "Edit" turns the
  body into the same form ("Edit your post", "Save" and "Cancel"). The
  1,000-character limit and the errors read as they do on the main form.
- **Delete** asks first in a Radix Dialog ("Delete your post?", with a
  `danger` "Delete" and "Cancel"). A deleted post with replies stays as
  "[deleted]" in `ink-muted` italic with no byline, actions or count, its
  replies under it; one without replies leaves the thread.
- **A post hidden by a moderator** is gone for readers, and so are the
  replies under it.

## Place page: admin tools

For admins only, the place page's sidebar ends with a dashed "Admin" box:
"Make this a venue…" on an imported store (so places can be added inside
a stadium or park) and "Merge into another place…" (find the twin, pick
it, give a reason). Both confirm in a dialog with a required reason for the
audit log; a merge lands on the kept place with a line saying what was
merged into it. In the merge dialog the matching places are one scrolling
list (at most 15rem tall) whose rows each take the height of their two
lines, never squeezed to fit; a polite `status` line above the list says
"3 places match" or "No other place matches." as the search answers.

## Composer: joining the day

Under the day and hour, a bordered note says what already exists for the
place that day: "This day is “Title”: 2 stories for the whole day, 1 story
at 13:00", then where the writer's story will go. Stories link only within
one arc, so the note steers toward the whole day unless the story belongs
to an hour. The arc title field shows only when nobody has written that day
yet; a day that has an arc keeps its title, never silently drops the
writer's. Search results say what a place is (a stadium's category rather
than "Store") and how many stories it holds, so friends pick the same entry.

## Place page header and reports

- Under the place name, one `text-sm` `ink-muted` word says what the place
  is, the same way search does: an imported store or mall with a category
  shows the category ("Sports"), never "Store" for a stadium.
- That category word is a link (`accent`, underlined, 44px tall) to the best
  days at every place of that kind, `/best/<slug>`. Its accessible name
  starts with the word it shows and says where it goes ("Sports: the best
  days at sports and recreation places"). A kind such as "Store" or "City"
  stays plain text.
- "Report place" opens the report dialog with an intro about places only
  (no talk of real names). When the reason is "Same place as another
  listing", the details field is required and is labelled "Which listing
  is it the same as?" with the hint "Paste its link or name it."

## Place page epic

Each day in the Ranked and Newest tabs is a row like a tale's: the day's
title as the link, the opening of its canon's first story (or its first story)
in the serif face clamped to two lines, then a `text-sm` line: "Canon" and
a crown when the day has one, followed by the canon lore's name in italics
when its members have named it ("Canon: *The long queue*"), the place, the
day, how many are in lore and the story count. The row is one shared
component (`ArcItem` in `web/src/components/reader/`), so the place page and
the best-days pages cannot drift apart.

## Best days at a kind of place

`/best/<slug>`, public, one page per place category, where the slug is the
category's enum name in lowercase with hyphens (`sports-and-recreation`).
It is the place page's epic across every place of one kind, and it reads
like one.

- **Heading.** The page title in Literata at the page-title size: "The best
  days at *plural*", where the plural says what the places are in words a
  reader uses ("The best days at sports and recreation places", "The best
  days at bars and clubs", "The best days at hotels"). The tab says the
  same. Under it, one line in Public Sans, `ink-muted`: "Days ranked by
  their canon across every place of this kind."
- **Tabs.** Ranked and Newest, the place page's `Tabs` with the same
  underline for the current tab, kept in the address as `?tab=newest` (the
  bare address is Ranked). There is no Tales tab and no genre filter: the
  page answers one question, which days at this kind of place people
  remember best.
- **Rows.** The place page's day rows, with the place named with its city
  ("Iron Gym, Tampa") because the list spans every city. "Show more" pages
  as on the place page.
- **Empty.** "No one has written about a day at *plural* yet." and a link
  back to the front page's search.
- **Unknown slug.** The not-found pattern of a missing place: "Kind of place
  not found", a line saying the address does not name one, and a link to
  the front page.

## Profile

Each perspective row is one link at least 44px tall. It leads with where and
when (place and day, `ink`, medium weight), then the day's title in the serif
face, then the story's own opening (its first part, clamped to two lines),
then its standing in `text-sm`, so a writer's stories are told apart even when
their days share a title.

## Place page sidebar

- **Were you in someone's story?** First in the sidebar: a search field
  (placeholder "gold paint, red umbrella…") over unclaimed descriptions at the
  place and below. Results quote the description in Literata, name the writer,
  place and day, and link "That was me" to the story with the claim dialog up.
  Under two characters nothing is searched; an empty result says so in a
  `status` line.
- The list of places inside follows it.

## Inbox and requests

- **Inbox rows.** A notification with a page is one link the full width of
  its row, at least 44px tall: the "New" tag, the sentence and the time
  all sit inside it, so a press anywhere on the row opens it. Only the
  sentence underlines on hover. Following the link, by any click or by
  Enter, marks the notification read. A notification with no page is plain
  text and has no hover.
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

## Following writers

- **Profile toggle.** Another member's profile carries a "Follow" toggle
  under the username, the same button as the place page's: ghost with a
  bell when off, `secondary` with a crossed bell reading "Following" when
  on, `aria-pressed` either way, and its accessible name adds the writer
  ("Follow wren"). A signed-out reader who presses it is sent to sign in.
  Beside it a `text-sm` `ink-muted` line counts followers ("1 follower",
  "12 followers"); the count shows to everyone, the toggle only on someone
  else's live profile, never on your own or a deleted one. A refusal (a
  block either way) is a `role="alert"` line in `error` under the toggle.
- **Settings: Writers you follow.** A section headed "Writers you follow"
  sits just above "Blocked users": one line of help ("A new story by a
  writer you follow lands in your inbox."), then one row per writer, newest
  follow first, each row the avatar (32px, round, only when set) and the
  username as a link to their profile, and an "Unfollow" `secondary` `sm`
  button on the right whose accessible name adds the writer ("Unfollow
  wren"). Unfollowing removes the row once the server confirms. With none,
  the section says "You do not follow any writers yet." Rows keep the 44px
  target and wrap long usernames at 375px.
- **Inbox.** A followed writer's story reads "*writer* wrote a new story in
  *day*." and opens the story; its settings toggle is "A new story by a
  writer you follow".

## Saving stories

- **Save toggle.** Every readable story's action bar carries a "Save"
  toggle in its second row, before Report, built like the Follow toggle:
  `sm`, at least 44px tall, `ghost` with an outline lucide `Bookmark`
  reading "Save" when off, `secondary` with a filled `Bookmark` reading
  "Saved" when on, `aria-pressed` either way, and an accessible name that
  adds the story ("Save otter's perspective", "Saved otter's perspective")
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
- **Way in.** The account menu lists "Saved" with a `Bookmark` icon first,
  above Settings.

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
  the groups says "Saved on this device." Every option is a native radio
  with a visible label, at least 44px tall.
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
  reads as one sentence then detail: "**ana** hid a perspective: *Rain at
  the deli*", with the time below in `ink-muted` and the reason, when
  there is one, quoted. An entry from the command line names "The command
  line" as its actor. A target that has a page links to it; a user target
  links to its dossier.
- In the report queue, the reporter's name links to their dossier.
- A report shows what its target said when it was filed, so an edit after
  the report cannot hide it: a `<details>` disclosure, "What it said when
  reported", closed by default, holding the text in `whitespace-pre-wrap`
  with the serif face for story and post text, at most 20 lines tall with
  its own scroll.
- **Undoing a decision.** A suspended account's dossier offers "Lift
  suspension…" beside its standing; a hide entry in the audit log
  (perspective, place or thread post) offers "Restore…". Both open the
  same small dialog: what is being undone, a required "Reason for the
  audit log", and a primary button naming the action. On success the
  dialog closes, a status line says what changed and takes focus, and the
  dossier or log re-reads, so the new entry appears at the top. A lift's
  line sits under the standing, where the button was; a resolve's line
  stays above the reports. A restore
  of something already visible says so in the dialog instead.

## App icon

The mark is the header's open book (lucide `BookOpen`) in `paper` on an
`accent` ink-blue square: `web/public/icon.svg` for tabs, PNGs at 180 (Apple
touch), 192 and 512, and a 512 maskable icon with the book inside the safe
zone. `manifest.webmanifest` installs Perspectiv standalone with `paper` as
its background and theme colour, and the page's `theme-color` follows the
light and dark themes.

## Legal and trust pages

`/terms`, `/privacy` and `/guidelines` are public reading pages: a title, a
"Last updated" line, prose at the reading measure with plain `h2` sections
and bullet lists, and a closing row of links to the other two. The footer
links all three on every page, and the sign-up terms box links them before
the age-and-agreement checkbox, so a keyboard user meets them first.

## Landing (the front door)

The landing page is the one surface that has to sell the idea to someone who
has never seen it, so it is the one page where Folio's quiet gives way to a
modern front door. Reading pages stay bookish; the front door does not have
to be. There is still no feed of posts (AC-006, A-049): the way in is a
place. The page shows one illustrated example and the places where people
are writing, each of which opens its place page.

- **Exceptions to Folio, here only.** Cards MAY use `rounded-xl` and a soft
  shadow (`shadow-sm` and an offset "stacked page" layer) to read as objects
  you can pick up. The accent MAY fill a tinted panel (`accent` at 6 to 10
  percent over `paper`) behind the example. Still no gradients, blur or
  motion.
- **Hero.** One promise, said plainly: "Same place. Same day. Every side of
  the story." in Literata 600, `text-4xl` rising to `text-6xl`, tight leading,
  then one sentence that says what Perspectiv is in Public Sans at
  `text-lg`, `ink-muted`.
- **The search is the call to action.** A large rounded field (`rounded-full`,
  `surface`, `control` border, 56px tall) labelled "Search places", with the
  placeholder "A store, mall, town or city" (short enough to show whole in
  a 375px field). Results drop
  below it as before. Under it, one line for newcomers: "Anyone can read.
  Sign up to write your side."
- **The example arc.** A labelled "Example" panel that shows the product
  working: a place chip and a day chip, then two perspective cards by two
  usernames from the same evening, each a few lines of prose in Literata. A
  phrase in one card is highlighted as a character, and a link line shows the
  other writer claiming it ("that was me"). Under the cards a lore badge says
  the linked stories became the canon of the day. On phones the cards stack.
- **How it works, as notes on the example.** Four short captions sit beside
  (desktop) or under (phone) the example, each with a lucide icon and no
  numbers: pinned to a place and a day; everyone writes their own side; claim
  the character who was you; linked stories become lore, and the favourite
  becomes canon.
- **Where people are writing.** Under the hero, up to six place cards from
  the most remembered days anywhere, one card per place: the place name and
  its city, the day's title, a two-line quote of its opening in Literata, and
  "Canon" (with the lore's name when it has one, as on the place page) or
  the story count. A card links to the place page, never straight
  to a story, so the way in stays place first. Two columns from `sm`, three
  from `lg`. The section is left out when no place has a story.
- **Places you follow** (signed in, when there are any) sits above "Where
  people are writing": one pill per place with a bell icon, linking to it.
  A place with stories published at it or below in the last seven days
  carries the count after its name ("Springfield · 3 new", the count in
  bold accent), and those places come first, most new first. When any are
  new, a line under the heading totals them ("5 new stories this week");
  counts stop at 100 and show as "100+".
  A place page carries a "Follow" toggle beside "Write about this place",
  `aria-pressed`, reading "Following" with a filled style when on.
- **Returning members** do not see the visitor hero, the example or the
  how-it-works notes. The page leads with a compact "Welcome back" block:
  the page heading "Welcome back, *username*" at the page-title size, then
  one line per thing waiting, each a count in bold and a link ("2 unread
  notifications · Open your inbox", "1 request waiting on you · Review
  requests"), or "Nothing is waiting on you." when both are zero. "Your
  perspectives" follows it, then the search field, then "Places you
  follow", "Where people are writing", "Browse by kind of place" and
  "Browse by state".
- **Browse by kind of place** sits above "Browse by state" for everyone: the
  heading "Browse by kind of place", then one pill per category that has at
  least one day, in the order of the place page's category chips, each its
  short label ("Sports", "Food & drink") linking to `/best/<slug>`. The
  pills look like the state pills. The section is left out when no category
  has a day, and while the list loads or fails, rather than showing an
  error on the front door.
- "Browse by state" closes the page for everyone.
- Nothing animates, and everything keeps the 16px gutter with no sideways
  scroll at 375px.

## Voice

Interface copy is plain and short: "Publish", "Claim character", "Vote to
remove". Canon is chosen by the people who wrote about the day, never by
"readers": only they can upvote or post in a lore's thread (A-012). Copy says
so ("the lore the people who were there favour most"), and a reader who
cannot vote or post is told who can instead of being urged to write. Book words appear where they are true (part, page, chapter of an arc)
and nowhere else.
