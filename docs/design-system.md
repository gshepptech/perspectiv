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

- **One heading style.** Every `h2` is the serif heading, including one that
  sits inside a bordered box ("How this day works", "Is this you?", "Were you
  in someone's story?", "How a day works"), which takes `text-lg` there. A
  heading MUST NOT switch to the interface face to look smaller.
- **Two-line previews** (an opening clamped to two lines in a row) set
  `hyphens: manual` (`hyphens-manual`). Automatic hyphenation in a narrow
  clamp breaks a word on most lines at 390px and reads as damage, not
  typesetting; running prose keeps `auto`.

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
- **Primary navigation** for a member is Search, Write, Requests, Inbox and
  the account menu, each an icon with a visible word; for a visitor it is
  Search, Sign in and Sign up. Below `sm` the word sits
  under its 20px icon in `text-xs` and the navigation may wrap under the
  wordmark; from `sm` the word sits beside the icon. Requests uses
  `UserCheck` and the account menu `CircleUser`, so no two items share an
  icon. The account item reads "Account" on phones and the username from
  `sm`. Unread notifications show as a count badge on the Inbox icon
  (`accent` fill, `accent-ink` text, bold), and the link's accessible name
  carries it ("Inbox, 2 unread"); with none unread there is no badge.
  Requests waiting on the member to accept or reject show the same badge
  on the Requests icon, named "Requests, 2 waiting" ("99+" in the badge
  past 99); with none waiting there is no badge. The badge hangs off the
  icon's top right corner and grows leftward over the icon. From `sm`,
  where the word follows the icon, a badged icon keeps a right margin
  wider than the badge's overhang, so no badge, "99+" included, covers a
  letter of its word; below `sm` the badge sits above the word, clear of
  it.
- **Header search.** "Search" (lucide `Search`) is the first navigation
  item on every page and at every width. It opens a Radix dialog titled
  "Search places", with one line under the title saying what it takes ("A
  place, or a place and its town: “starbucks tampa”."), that holds the
  front page's place search (see Place search, which gives its keys): the
  labelled field, which takes focus on
  open, pinned 16px from the top on phones and a tenth of the way down from
  `sm` so it stays put as results arrive, and the same results, "more"
  line and "Not here? Add …" link. Each result links to its place page and
  closes the dialog, whether clicked or opened with Enter. Pressing "/" opens it from anywhere except while focus is in
  a text field, text area, select or editable text, and never with Ctrl,
  Alt or Meta held. Below `sm` the dialog is the viewport width less the
  16px gutter and at most the viewport's height, the results scrolling
  inside it. Closing it, by Escape, Close or a result, empties the field
  and returns focus to the Search button unless a result was followed.
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
| `rule` | `#D6D1C7` | `#46413B` | decorative hairlines only | n/a (non-informational) |
| `control` | `#8A8478` | `#7A746A` | input and control borders | 3.4 / 3.2, 3.9 / 3.6 (3:1 for UI components) |
| `accent` | `#2B4C7E` | `#9DB8E6` | links, primary buttons, focus ring, current page | 7.9 / 7.3, 9.1 / 8.3 |
| `accent-ink` | `#FFFFFF` | `#12223A` | text on `accent` | 8.6, 7.9 |
| `success` | `#2F6B3A` | `#7CC48A` | accepted claims, saved | 5.9 / 5.4, 8.8 / 8.0 |
| `warning` | `#8A5A00` | `#E0B455` | pending, expiring drafts | 5.4 / 5.0, 9.4 / 8.6 |
| `error` | `#A3322A` | `#F08A80` | validation and request errors | 6.3 / 5.8, 7.5 / 6.9 |
| `info` | `#1F5F7A` | `#7FC4DD` | neutral notices | 6.5 / 6.0, 9.4 / 8.6 |

Lamplight's `rule` sits at about 1.8:1 on `paper` (1.7:1 on `surface`),
heavier than Paper's 1.4:1, because a dark hairline at the same ratio
vanishes on most screens. It stays well under `control` (3.9:1), so a
hairline never reads as a field's border, and nothing a reader must see to
operate a control is drawn in `rule`.

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
  A caller with somewhere to go past an end (a lore's book, between
  chapters) gives the Reader that edge's action and label instead, and the
  control turns there rather than sitting disabled.
  The row is a three-column grid (`grid-cols-[1fr_auto_1fr]`): previous at the
  start, the label centred on the measure whatever the buttons' widths, next
  at the end.
- **The part heading** ("Part 2") is set like a book's part number, not a
  form label: the serif face in italic, `text-base`, weight 400, `ink-muted`,
  with no capitals and no letter-spacing. It stays an `h2`, and it is the
  same on one page and paged.
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
- **The cast is a plain list**, one character to a line, each a tag in the
  sense of Flairs and tags below: a 16px icon (lucide `UserCheck` once
  someone claimed it, `UserRound` while nobody has), the description in the
  serif face, then " · " and the claimant's name. A username wraps only
  where it must (`overflow-wrap: break-word`), never mid-name when the line
  has room for it whole.

## Reacts

Reacts are a reader's aside, not the story's headline, so the row is quiet
until someone uses it.

- One toggle per emoji in the fixed set, borderless, 44px tall, in a `group`
  named "Reacts". Hover gives it a `surface` fill; nothing else outlines it.
  The first toggle is flush with the text column, as a flush ghost button
  is.
- An emoji the reader has not used is drawn greyscale at 70 percent opacity
  (`grayscale opacity-70` on the glyph only); the reader's own react is in
  full colour with its count in bold `ink` and a 2px `accent` underline
  under the toggle, so the choice is never shown by colour or opacity alone.
- A count of zero is not shown; the toggle shows the glyph alone. Counts
  above zero are `text-sm` `ink-muted`.
- The accessible name keeps the count ("Heart, 0", "Heart, 3") and
  `aria-pressed` carries the reader's own react, whatever is drawn.

## Lore page: stories

The lore page's stories are chapters of one account, not cards on a board.

- Each story is a plain `<article>` under the one before it, set apart by a
  hairline and space (`border-t border-rule pt-8`; the first, right under
  "The stories", has neither), with no box, fill or shadow, so the prose
  takes the full measure at 390px.
- Its heading ("otter's story", the name a link) is the serif `h3` at
  `text-xl`; the genre sits on the same line in `text-sm` `ink-muted`.
- The action row under it is the story page's.

## Page titles

Every route sets `document.title` through one hook, `useDocumentTitle` in
`web/src/lib/title.ts`, as "<what the page shows> · Perspectiv": a story is
"<arc title> by <author>", an arc its title, a place its name, a lore its
name (an unnamed lore "Unnamed lore in <day title>", never the day's title
alone), a profile the username, and a fixed page its heading
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
  and Mark all read, a request's links to its two stories).
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
| `Button` | native `<button>` (Radix `Slot` for `asChild`) | variants `primary`, `secondary`, `ghost`, `danger`; sizes `sm`, `md`; `flush` (below) |
| `Input`, `Textarea` | native elements | `control` border, `surface` fill, `aria-invalid` turns the border `error` |
| `Dialog` | `@radix-ui/react-dialog` | `surface` panel, full width minus the gutter on phones |
| `Tabs` | `@radix-ui/react-tabs` | the epic's Ranked, Newest and Tales tabs |
| `Select` | `@radix-ui/react-select` | genre, report reason |
| `Tooltip` | `@radix-ui/react-tooltip` | supplementary only; never the sole label |
| `DropdownMenu` | `@radix-ui/react-dropdown-menu` | account menu, per-perspective actions |
| `Form` | `react-hook-form` with `@radix-ui/react-label` | `FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`; errors render inline under the field |

Icons are `lucide-react`, imported by name, at 16px in interface text and 20px
in navigation, with `aria-hidden` when a text label sits beside them.

- **Flush ghost buttons.** A `ghost` button's padding is only there for its
  hover fill, so one that starts a row of the text column (Follow on a
  profile, Save under a story, a lore's "Report this name") takes `flush`:
  a negative start margin equal to its padding, applied only while it is
  its row's first child, so its icon lines up with the text above it. A
  `flush` button later in a row keeps its place. A toggle that turns
  `secondary` when on (Follow, Save) is flush only while it is `ghost`,
  since a border MUST NOT hang outside the text column.

## Flairs and tags

Flairs (Canon, Contender, "In this lore", a profile's "In 2 canons" and "In
5 lores") and cast tags are words, not objects: `text-sm`, with a 16px icon
where one exists (`Crown` for canon, `Swords` for a contender, `Users` for
lore membership, `UserCheck` or `UserRound` for a cast tag), and no pill,
fill or border. Canon is `accent` and medium weight; the rest are
`ink-muted`, or `ink` where the word is the point. A row of them wraps with
a gap of 12px and never scrolls sideways. The one exception is the place
page's "New" label, which keeps its 1px `accent` border (Place page epic).

## Arc page (the day)

The arc page is an index, like a subreddit's front page: it organises the
day's stories so a reader can see what there is and pick one. Reading happens
on the story's own page, so the arc page MUST NOT carry a story's full action
bar (cast, reacts, claim, report); those live on the story's page and on its
lore page.

- **Header:** the place chain (as the place page's breadcrumb: `text-sm`
  `accent` underlined links, each 44px tall, separated by an `ink-muted`
  "›"), the arc title, then one metadata line:
  the place-local day and who started it. The primary action ("Write your
  story", or "Read yours" for a participant) sits in the header at every
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
  the only place on the arc page a reader acts on a story. A reader who may
  not vote here (signed out, or with no story of this day) gets no button on
  the rail, only a plain `control` arrow over the count, never an arrow that
  leads somewhere or does nothing when pressed.
- **Who can upvote, said once.** For that reader, a bordered `surface` note
  sits at the top of the feed column, above the canon, with the words the
  story page's upvote note uses: "Only people who wrote about this day can
  upvote its stories, so the people who were there choose its canon."
  ("this hour" on an hour arc), then "Were you there? Write your side" as a
  plain text link to the composer for this place and day. Signed out, an
  "Already wrote yours? Sign in" text link follows, returning to the day.
  A day written only in its hours has no voting rows and no note.
- **A day written only in its hours** has no stories of its own: the page
  leads with the hour timeline and says so, instead of an empty canon.
- **Standing alone:** stories in no lore (waiting for a link, or tales) are one
  section of story rows below the lores, each row carrying its standing as a
  flair word. Its intro MUST NOT say a tale can never link: a story still
  unlinked a week after it was published is called a tale, and an accepted
  claim still links it into a lore.
- **Is this you?** At the top of the sidebar: the people described in the
  day's stories whom nobody has claimed, each quoted in Literata with "in
  otter's story" and a "That was me" link that opens the story with the
  claim dialog up. Six at first, then "Show all". Left out when there are
  none; the viewer's own characters are never listed. Below `lg`, where the
  sidebar falls under every story, it is a closed one-line disclosure under
  the header instead ("Is this you? 2 people nobody has claimed yet").
- **Not yet able to claim or upvote.** On a story's page, a reader with no
  story in its arc, or a signed-out reader, still sees "Claim a
  character" (when the story has someone open to claim) and "Upvote".
  Pressing either opens a bordered `surface` note in place under the
  actions instead of a dialog or a silent redirect: "To say this was you,
  write your side of this day first. Your story and theirs link once they
  accept." (for upvote: "Only people who wrote about this day can upvote
  its stories, so the people who were there choose its canon."). For a
  claim, a primary "Write your side" link to the composer for that place and
  day follows; for an upvote only a plain "Were you there? Write your side"
  text link, since most readers were not. Signed out, the claim note shows
  "Sign in" and "Create an account" links that return to the story; the
  upvote note keeps "Were you there? Write your side" (the composer asks
  them to sign in first) and adds "Already wrote yours? Sign in", the same
  note the day page shows. On an hour arc the notes say "this hour". Arriving from
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
  stories and lores, who may do what here in one sentence, a "Read more"
  link to the guide (`/how-it-works`) closing "How this day works", and
  "Report place".
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
  moves no focus, and a visually hidden polite `status` names the new
  order ("Most upvoted first." or "Newest first."). Replies are always
  oldest first, whatever the sort.
- **A post** is an `<article>` named by its author ("otter's post"). Its
  byline is one `text-sm` `ink-muted` line: the author link (44px tall,
  as in a story's byline), "In this lore" flair for a member, the time, and "· edited" when the author
  changed it (the time of the edit in the `<time>` title). The body follows
  in the interface face, wrapping anywhere, keeping the writer's line breaks.
- **Action row** under the body, every control at least 44px tall and
  wrapping at 375px: the upvote toggle (`ArrowBigUp`, the count beside it,
  `aria-pressed` for the viewer's own vote, named "Upvote otter's post");
  "Reply"; then "Edit" and "Delete" on the viewer's own post, or "Report"
  on anyone else's. Someone who may not vote sees the count without the
  toggle, and never sees the toggle on their own post. After the viewer's
  own vote a visually hidden polite `status` gives the result and the new
  count ("Upvoted. 5 upvotes." or "Upvote removed. 4 upvotes.").
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
  A new post or reply shows at once and takes focus: a reply at the end of
  its group, a new post at the end of the list until the thread re-reads
  it into its place in the sort.
- **Delete** asks first in a Radix Dialog ("Delete your post?", with a
  `danger` "Delete" and "Cancel"). A deleted post with replies stays as
  "[deleted]" in `ink-muted` italic with no byline, actions or count, its
  replies under it; one without replies leaves the thread.
- **A post hidden by a moderator** is gone for readers, and so are the
  replies under it.
- **Every post has an address.** A post's `<article>` carries
  `id="post-<post id>"` and `tabindex="-1"`, so `/lores/<id>#post-<post
  id>` opens the lore at that post. The profile's Thread posts rows and the
  inbox's "replied to your post" row link there (the inbox to the reply).
- **Arriving at an anchor.** The page waits until the lore, its stories
  and the thread's first page have loaded, so nothing above the target
  moves once it is reached, then scrolls the target into view without
  animation and focuses it: the post's article for `#post-…`, the
  thread's heading for `#thread`. A post not on the first page is looked
  for in the Top order a page at a time, up to 10 pages (200 posts); a
  reply is found when it is among the replies that come with its post.
  The post found takes a 2px `accent` outline, offset 4px, for two seconds;
  it appears and goes without a transition, so it is the same under
  reduced motion. When the post cannot be found (deleted, hidden, among
  the later replies of a long branch, or past the tenth page), focus goes
  to the thread's heading and a `text-sm` `ink-muted` line under it, which
  the heading's `aria-describedby` names, says "That post could not be
  shown here. It may have been deleted, or it is further down among the
  replies to its post."
- **Thread link at the top.** Beside "Read as a book" in the lore's header
  sits a link "Thread · 3 posts" (lucide `MessagesSquare` at 16px,
  `accent`, underlined, 44px tall) to `#thread`, which lands as above.
  The count is every post a reader of the thread sees, replies included,
  never a deleted or hidden one: "1 post" for one, "no posts yet" for
  none. The two links wrap onto two lines at 375px.

## Lore as a book

The founder's picture of the app is a novel, and of an entry "a page in a
book". A lore read as a book is that picture: the lore's stories bound in
the lore page's order, one chapter each, read a page at a time. It is a
reading surface, so the principles, the measure and the motion rules above
hold on every page of it; the actions, the claim and react controls and the
thread stay on the lore page.

- **Entry.** Under the lore page's counts, a "Read as a book" link
  (lucide `BookOpenText`, 44px tall) opens `/lores/:id/book`, a public
  route.
- **Pages, in order:** the title page, the contents, every chapter a part
  at a time, then the closing page. The address names the page:
  `/lores/:id/book` is the title page, `?at=contents` the contents,
  `?chapter=2&part=3` a chapter's part (a missing part is part 1; a
  chapter or part past the end opens the last), and `?at=end` the closing
  page. Each turn pushes a history entry, so Back returns to the previous
  page and a shared address opens where it was copied.
- **Title page.** Centred in the measure: the lore's name in the serif face
  (`text-3xl` from `sm`), or "Unnamed lore" while its members have not
  named it; the day and place on one `ink-muted` line as on the lore page;
  then "Written by" and the writers of its chapters as profile links. A
  returning reader gets a primary "Continue where you left off" link with
  the place under it in `text-sm` ("Chapter 2, part 3 of 4").
- **Contents.** "Contents", then an ordered list of the chapters, each one
  row link at least 44px tall, on three lines: "Chapter 2" in `text-sm`
  `ink-muted`; the chapter's title in the serif face at `text-xl`; then
  "by otter · 2 parts" in `text-sm` `ink-muted`. The writer's name there is
  plain text, since the row is already one link. The lore page's "Who was
  who" follows the list.
- **A chapter is titled by who its writer was.** A chapter's title is its
  writer's role in the lore, read from "Who was who": the label of a
  character in another chapter that the writer claimed and its author
  accepted, with its first letter capitalised ("The woman crying into a hot
  dog wrapper"). A writer who claimed more than one takes the first, in
  chapter order and then the character's order in its story. A writer no
  one has placed is titled by their story's first words, at most 40
  characters, cut after the last whole word that fits and ending "…" when
  the story goes on ("The rain came in sideways and the…"). The username is
  never the title; it is the byline.
- **A chapter** is the story page's Reader with the measure, the part
  headings and the 150ms cross-fade unchanged. Its byline is "by otter",
  a profile link, and the genre, in `text-sm` `ink-muted`.
- **A chapter opens like one.** The chapter's header is centred, with
  generous space above it (`pt-10`, `pt-16` from `sm`): "Chapter 2" on its
  own line in `text-sm` `ink-muted` in the interface face, then the
  chapter's title under it in the serif face at `text-3xl`, then the
  byline. Both lines are the one `h1`, named outright with `aria-label`
  "Chapter 2: The guy in the yellow raincoat", since readers join two block
  lines differently.
- **The page-turn row stays put.** Every page of the book (title, contents,
  each part of a chapter, the end) sits in a page frame at least the
  viewport's height less 12rem (`min-h-[calc(100dvh-12rem)]`), with its
  previous and next row pushed to the frame's foot, so the controls sit in
  the same place from one page to the next while a page's text fits. The
  row above the frame ("Back to the lore", "Contents") keeps its 44px
  height on every page, empty on the closing page, so the frame starts at
  the same place too. A chapter always pages by part:
  the lore page already shows every story whole, so the book offers no
  "Read all parts on one page" toggle. The label between the part controls
  reads "Chapter 2, part 3 of 4".
- **Next and Previous cross chapters.** A chapter's part controls go
  exactly where the arrow keys go. On a chapter's last part, Next reads
  "Next chapter" and opens the next chapter's first part, or "The end" on
  the last chapter and opens the closing page. On part 1, Previous reads
  "Previous chapter" and opens the previous chapter's last part, or
  "Contents" in chapter 1 and opens the contents. These edge labels show
  at every width, a phone's included, where the plain "Previous" and
  "Next" are icons only; the button's name is its label. Below `sm` an
  edge control drops its arrow, since its words say which way it goes.
- **The book's page-turn row on a phone.** "Previous chapter", "Next
  chapter" and "Chapter 2, part 1 of 1" do not fit on one line at 375px,
  so below `sm` every page of the book (chapter or not) puts the position
  on its own centred line, with Previous and Next on the line under it at
  the two ends; from `sm` the row is the one line the story page has. The
  story page's own row is unchanged at every width. No part control in
  the book is ever disabled. The Reader takes the edge actions from its
  caller; on the story page a story's first and last parts keep their
  disabled Previous and Next.
- **No second row of chapter links.** With Next and Previous crossing
  chapters, a "Next chapter: …" row under the last part would repeat the
  control above it, and its "Previous chapter" would open a different part
  than Previous does. The jump list is the contents, one tap away in the
  row above every chapter, and it names each chapter.
- **Closing page.** "The end", one line naming the lore, and two links:
  "Back to the lore" and "Read its thread", which opens the lore page at
  `#thread` and moves focus to the thread's heading.
- **Turning pages.** The title, contents and closing pages carry the same
  previous and next row a chapter does, as links ("Previous page", "Next
  page"), with the position centred between them as on a chapter.
  `ArrowLeft` and `ArrowRight` turn
  pages everywhere in the book while focus is not in a field, across
  chapter boundaries too: forward from a chapter's last part opens the next
  chapter's first, back from a first part opens the previous chapter's
  last. The book owns the keys on every page (the Reader's own arrow
  keys are off there). Keys and a chapter's part controls take one turn
  path: a press, by key or by button, made while the previous turn is
  still landing waits for it, so every press turns exactly one page.
- **Focus and announcement.** After a turn, focus moves to the new page's
  heading: a part's "Part N" heading within a chapter, otherwise the page's
  level-one heading. One visually hidden polite `status` line, outside the
  page that changes, names the new place: "Title page", "Contents",
  "Chapter 2, part 3 of 4" or "The end".
- **Remembered place.** The reader's chapter and part are kept per lore on
  the device in `localStorage`, read and written inside try/catch, by the
  story's id, so a chapter that moves still resolves. The place MUST NOT be
  sent to the server. Reaching the closing page forgets it; a blocked store
  only means no "Continue" offer.
- **Stories that cannot be read.** A deleted or hidden story is left out
  of the book exactly as the lore page leaves it out: it gets no chapter
  and its writer is not listed. With none readable, the contents say "None
  of this lore's stories can be read right now."
- **Document title:** the lore's title as the lore page sets it, then ",
  as a book".

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

## Place search

The front page's search and the header's Search dialog are one control: a
WAI-ARIA combobox over a list of places. Focus stays in the field while the
keys move through the list, so a reader can type, look and open a place
without leaving it.

- **Roles.** The field, labelled "Search places" and inside the `search`
  landmark, is `role="combobox"` with `aria-autocomplete="list"`. Its
  `aria-controls` names the results, a `role="listbox"` named "Matching
  places", and `aria-expanded` is `true` only while that list shows at
  least one place. Each result is an `option`: a link set in that role, so
  a pointer can still open it in a new tab, and out of the tab order
  (`tabindex="-1"`). The field's `aria-activedescendant` names the active
  result, and is absent when none is active.
- **Keys.**
  - ↓ makes the next result active and ↑ the previous one, wrapping at
    either end; ↓ from the field with none active takes the first, ↑ the
    last. ↓ or ↑ also reopens a list that Escape closed.
  - Enter opens the active result's place page, or the first result's when
    none is active. Pressed before the results for the text in the field
    have arrived, it opens the first of them once they do, and does
    nothing when none match. Enter never submits a form or reloads the
    page.
  - Escape on the front page closes the list (the text stays), and a
    second Escape clears the field. In the header dialog Escape closes the
    dialog, which empties the field (see Header search).
  - Typing, ←, →, Home and End leave no result active and do what they do
    in any text field; Home and End move the caret to the start and end of
    the text, never to the first or last result.
  - Tab leaves the field for the next control, the "Not here? Add …" link.
- **The active result** takes the `surface` fill and a 2px `accent`
  outline inset by 2px, the focus ring's colour, so it is unmistakable
  without relying on opacity; a hovered row takes the fill alone. When the
  list scrolls (the dialog on a phone), the active row is scrolled into
  view.
- **Announcements.** One polite `status` line, always mounted and visually
  hidden while places show, says what the list holds once per answer:
  "Searching…", "4 places match “starbucks tampa”.", "No places match
  “Atlantis”." or, at the cap, "Showing the first 20 places that match
  “tampa”. Add a town or state to narrow it." A screen reader announces
  the active result from `aria-activedescendant` as its whole row, the
  name then its second line. Moving through the list adds nothing to the
  status line.
- **What and where.** A result's second line says what the place is and
  where, nearest first, then its story count. A place inside a mall or a
  venue names that first, after "at", so two same-named stores in one town
  can be told apart: "Food & drink at International Plaza in Tampa,
  Florida · 2 stories"; elsewhere it reads "Food & drink in Tampa, Florida
  · 2 stories".
- **More.** The search stops at 20 places. When more match, the line
  "Showing the first 20. Add a town or state to narrow it." (`text-sm`,
  `ink-muted`) follows the list. A town, a city or a state's full name
  narrows it ("tampa florida", "starbucks, tampa"), and so does a state's
  two-letter postal code at either end ("starbucks fl", "FL starbucks"),
  which also keeps a town to that state ("publix tampa fl", "publix,
  tampa, FL."). A code that is also a word ("in", "ok", "me") is read as
  a state only as the last word after a name, so "starbucks in tampa"
  still means Tampa.
- **Adding the place that is missing.** The "Not here? Add …" link under
  the results opens the composer's add form. When the server read the
  search as a name and the city, town or state it is in, the response
  carries that split, and the link offers the name and the place apart:
  "Not here? Add “Joes Crab Shack” in Tampa, Florida". It opens
  `/compose?add=<name>&in=<locality id>`, so the form starts with that
  name and with "Where is it?" set to that locality. A name typed all in
  lower case is suggested in title case, as the importer writes such
  names (minor words such as "of" and "the" stay lower case inside a name,
  and a letter after an apostrophe stays lower case: "Joe's Bar-B-Que");
  a name with any capital is kept as typed. Without a split the link
  offers the whole search as the name and leaves "Where is it?" empty.

## Composer: place picker

"Where were you?" lists the matching places as the front page does: each
row is the name, then one `text-sm` `ink-muted` line saying what and where
it is and how many stories it holds ("Food & drink in Tampa, Florida · 2
stories", "No stories yet"), so a writer can tell which of two same-named
listings their friends write in. Rows keep the server's order, which puts
places with stories first within each match tier. When the search reaches
its cap, the line "Showing the first 20. Add a town or state to narrow it."
follows the list. "Add a place" opens the add form from the search the
same way the front page's link does: the name part, title-cased when it
was typed all in lower case, and the city, town or state the search read.

## Composer: day and hour

"Day of your visit" and "Hour" sit side by side from `sm`, each control
44px tall (the Hour select's trigger is `h-11`) and top-aligned with the
other, whatever the length of the help line under either. The day's help
line reads "Today at *place* is October 16, 2026." ("at", since a place is
as often a shop as a city).

**Days already written about.** The composer opens on today, but most
writers come to a day that has already happened, often one their friends
wrote about. So under the two fields, across both columns, the place's
recent days with stories are offered as one-tap choices, read from the
place's Newest epic (GetEpic, NEWEST) and limited to days filed at this
place itself, never at a place under it, five at most:

- A `text-sm` `ink-muted` line, "Days already written about here", labels
  the list (`aria-labelledby`).
- Each choice is a `button` (not a link: it sets the form, it goes
  nowhere) on its own line, full width, at least 44px tall, `rounded-md`
  with a `control` border, `text-sm` text wrapping as it needs: the short
  day ("Oct 3", with the year only when it is not this year), the hour for
  an hour day ("Oct 3, 14:00", left off when the title already ends with
  it), the day's title, then the count, joined by middots: "Oct 3 · Fourth
  quarter, sixty thousand people · 14 stories".
- Choosing one sets "Day of your visit" to that day and "Hour" to that hour
  (or "Whole day"), so the joining note below answers at once.
- The choice matching the form's day and hour is marked unmistakably:
  `aria-pressed="true"`, an `accent` border and a leading `Check` icon
  (`aria-hidden`), never by opacity alone. The others carry no icon and
  keep their text where it was.
- The list is left out while it loads, when it fails, when the place has
  no days yet, when the day and hour are fixed (joining a story), and once
  the story is published.

## Composer: joining the day

Under the day and hour, a bordered note says what already exists for the
place that day: "This day is “Title”: 2 stories for the whole day, 1 story
at 13:00", then where the writer's story will go. Stories link only within
one arc, so the note steers toward the whole day unless the story belongs
to an hour. The arc title field shows only when nobody has written that day
yet; a day that has an arc keeps its title, never silently drops the
writer's. Search results say what a place is (a stadium's category rather
than "Store") and how many stories it holds, so friends pick the same entry.

**Resuming a draft.** A writer has at most one draft in mind per place,
day and hour, and the composer finds it wherever they come in:

- Opening the composer for a place, day and hour where the writer already
  has a draft resumes that draft instead of starting a blank one, and the
  usual `info` note at the top says so: "You started a draft for this day,
  so it is open again." The address then names the draft, so a reload
  reopens it.
- The same lookup runs again whenever the day or hour changes in the form
  (typed, picked from the hour list or chosen from "Days already written
  about here"), not only on load. When the form has nothing written yet,
  the draft found opens in place: its text, genre, title and characters
  fill the form, focus stays where the writer is, and the same note
  appears at the top.
- When the writer has already written something in this form and another
  draft of theirs exists for the newly chosen day or hour, nothing they
  typed is replaced. A bordered `warning` note under the day and hour says
  "You already have a draft for this day." ("this hour" for an hour) with
  an "Open that draft" link; their own text keeps saving as its own draft.
- When several drafts exist for one place, day and hour, the one changed
  most recently opens, and the note says so: "You started more than one
  draft for this day, so the one you changed last is open. The others are
  under Your stories on the front page."

**Clearing a draft.** Autosave waits while every part is empty ("Your
draft saves once a part has text"), and the server refuses an empty draft,
so a writer who clears a saved draft still has its old text stored, and it
comes back the next time the draft resumes. The composer says so and
offers to delete it:

- When a saved draft (one with an id, never a published story) has every
  part empty, an inline note appears under the parts, before "Add part":
  a bordered `surface` box (`rule` border, no status colour, since nothing
  has gone wrong) reading "This draft is empty. Delete it?" with two
  buttons, a `danger` "Delete" and a `secondary` "Keep writing", each 44px
  tall, in a row that wraps when it does not fit. The genre, title and
  characters do not count as text, as they do not for autosave. The note
  is a polite live region, so it is announced as it appears, and focus
  stays in the part the writer was clearing.
- "Keep writing" closes the note and moves focus to Part 1. The note
  comes back only after a part has had text again and every part is
  emptied again.
- "Delete" waits for any save in flight, then deletes the draft
  (`Delete`, the RPC the front page's draft rows use) and opens the
  composer again on the same place, day and hour, as if opened there
  fresh: the address drops the draft (`/compose?placeId=…&date=…&hour=…`,
  keeping `return` and `invite`), the form is blank, and the notice at the
  top reads "Your empty draft is deleted." and takes focus, since the
  button that had it is gone. Should another draft of the writer's exist
  for that day and hour, it resumes, as Resuming a draft says. While the
  delete is in flight the button is pending; when it fails, a
  `role="alert"` line in `error` under the buttons says "Not deleted. Try
  again in a moment." and the note stays.
- A draft whose parts are all empty is never saved: autosave waits until a
  part has text, and the save status line reads "Your draft saves once a
  part has text." until then. The server refuses an all-empty draft as well, so an
  empty draft can never sit beside a real one and be the one that reopens.

## Place page header and reports

- Under the place name, one `text-sm` `ink-muted` word says what the place
  is, the same way search does: an imported store or mall with a category
  shows the category ("Sports"), never "Store" for a stadium.
- **The action row** under it: "Write about this place" first, as the
  `primary` button, since writing is what the page is for; then "Follow"
  and "Report place" (and "Add a place here" where the place holds
  others) as `ghost` buttons of one size, `sm` in type and 44px tall.
  "Follow" is a toggle, `aria-pressed`, reading "Following" in the
  `secondary` style when on.
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
day, and the count: "14 stories · 12 linked", the linked part left out at
zero. The row is one shared
component (`ArcItem` in `web/src/components/reader/`), so the place page and
the best-days pages cannot drift apart. The list's accessible name is "Days".

- **What is new.** On the Newest tab only, a day with a readable story
  published in the last seven days (the window a followed place counts its
  new stories in) carries a small "New" label after its title: `text-xs`,
  medium weight, `accent` text in an `accent` 1px rounded border, never
  colour alone. Ranked and Tales stay unmarked.
- **A duplicate listing points to the live one.** A store or mall with no
  stories that is a second listing of a place people do write about (the
  `twin` GetPlace returns, found by placectl dedupe's name and distance
  rule) says so inside its empty note, as one line under "No one has
  written about *place* yet.": "People write about this place at
  *twin*.", the twin's name an `accent` underlined link 44px tall to its
  page. The "Write the first story" button stays for a reader who means
  this listing.

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
- **Order changes are announced.** Switching tabs moves no focus; a
  visually hidden polite `status` names the new order ("Ranked by canon." or
  "Newest first.").
- **Tabs.** Ranked and Newest, the place page's `Tabs` with the same
  underline for the current tab, kept in the address as `?tab=newest` (the
  bare address is Ranked). There is no Tales tab and no genre filter: the
  page answers one question, which days at this kind of place people
  remember best.
- **Rows.** The place page's day rows, with the place named with its city
  ("Iron Gym, Tampa") because the list spans every city. "Show more" pages
  as on the place page.
- **Empty.** "No one has written about a day at *plural* yet." and a link
  back to the front page's search, 44px tall.
- **Unknown slug.** The not-found pattern of a missing place: "Kind of place
  not found", a line saying the address does not name one, and a link to
  the front page.

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

## Place page sidebar

- **Were you in someone's story?** First in the sidebar: a search field
  (placeholder "red umbrella…", short enough to show whole in the 16rem
  sidebar) over unclaimed descriptions at the
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
     automatically." under the field.
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
  two-factor sign-in" in the action filter.

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
links all three on every page, after "How it works" and before
"Appearance", and the sign-up terms box links them before the
age-and-agreement checkbox, so a keyboard user meets them first.

## How Perspectiv works (the guide)

`/how-it-works` is a public reading page that explains the whole product
to a first-time visitor. In two walkthroughs, newcomers could not work
out day, story, lore, canon, claims, voting out or hours from the
four-line summaries, so this page says each rule exactly, once, in the
book's voice, and follows one worked example the whole way through.

- **Shape.** One column at the reading measure, set like the legal pages:
  the `h1` "How Perspectiv works" (`text-2xl`, `text-3xl` from `sm`), a
  two-sentence lede in Literata `text-prose` `ink-muted`, then the front
  page's facing-pages example (the same `figure`, wren and otter at
  Moonbean Coffee), then the sections. No "Last updated" line, no cards,
  boxes, fills or icons beyond the example's own.
- **Sections**, each a serif `h2` at `text-xl` over prose in Literata
  (`prose-body`), in this order: "A place and a day", "Writing your
  story", "Characters and “that was me”", "How stories link into a lore",
  "How canon is chosen", "Voting a story out", "Hours", "What readers can
  do", and "Your privacy". The readers' section is a bulleted list; every
  other section is two or three short paragraphs.
- **The worked example.** Each section but the last two ends with one
  paragraph that carries the example forward (wren titles the day and
  writes two parts, otter writes his side and claims "the guy in the yellow raincoat", two more
  people make the lore four, the day's writers upvote it into canon, what
  a vote-out would take, and what an hour would have changed). It is set
  off by a 2px `rule` left border and `pl-4`, in the same face and colour
  as the prose, so it reads as an aside, not a warning or a quote.
- **Rules said exactly**, from the code, never from the summaries: the
  numbers come from the same constants the rest of the client uses (4
  people for canon and for a vote-out, 10 parts of 300 words, 10
  characters, a week before an unlinked story stands alone). Canon is
  chosen by the people who wrote about the day and never by readers; only
  they upvote, claim and post in a lore's thread. The page MUST NOT promise
  anything the server does not do.
- **Privacy** is one sentence: other people see only your username, and
  your email address is never shown, with "privacy notice" a link to
  `/privacy`.
- **Ways in.** The footer's first link, "How it works", on every page; a
  "Read more" link at the end of the front page's "How Perspectiv works"
  list; a "Read more" link at the end of the day page's "How this day
  works" section; and a "Read more" link at the end of a new member's
  "How a day works" box. Each "Read more" is a `text-sm` `accent`
  underlined link, 44px tall, whose accessible name is "Read more about
  how Perspectiv works", so the visible words begin its name.
- **Title:** "How Perspectiv works · Perspectiv". The sitemap's `pages`
  file lists `/how-it-works`.

## Landing (the front door)

The founder's picture is that the app "starts out looking like a novel
book", so the front door is a book's title page set in Folio, not a product
hero: the same paper, type, hairlines and quiet as every reading page, with
no exceptions to the principles above. There is still no feed of posts
(AC-006, A-049): the way in is a place. The page shows one example, as two
facing pages, and the places where people are writing, each of which opens
its place page.

- **One column.** The page, signed in or out, is one centred column at most
  48rem wide (`max-w-3xl`), inside the 16px gutter. Sections are set apart
  by space and `rule` hairlines; there are no cards, rounded panels,
  shadows, tinted fills or offset "stacked page" layers.
- **Title page.** Centred, with generous space above it (`pt-10`, `pt-20`
  from `sm`): the promise "Same place. Same day. Every side of the story."
  as the `h1` in Literata 600, `text-4xl` rising to `text-5xl` from `sm`,
  balanced across its lines; a short centred `rule` hairline (4rem) under
  it, as a title page's ornament; then one sentence that says what
  Perspectiv is, in Literata at `text-prose`, `ink-muted`, at the measure.
- **The search is the call to action.** Under that sentence, a 56px field
  (Folio's `Input`: `rounded-md`, `surface`, `control` border, no shadow),
  centred and at most `max-w-xl` wide, labelled "Search places", with the
  placeholder "A store, mall, town or city" (short enough to show whole in
  a 375px field). Results drop below it, left-aligned. Under it, one
  centred `text-sm` `ink-muted` line for newcomers: "Anyone can read. Sign
  up to write your side."
- **A place and its town.** The search reads a name with the city or town
  it is in, either way round or after a comma ("starbucks tampa", "tampa
  starbucks", "starbucks, tampa"), and lists the places of that name in
  that town first, the best-known town of that name first. A state's full
  name works the same way ("tampa florida"), after a city or town of that
  name. The search stops at 20 places; when more match, the line "Showing
  the first 20. Add a town or state to narrow it." (`text-sm`, `ink-muted`)
  follows the list, and the results' status line says it too, so the list
  never stops silently. The field's keys are in Place search.
- **The example: two facing pages.** A `figure` that shows the product
  working, as a book opened flat. Its caption is one `text-sm` `ink-muted`
  line, "Example · Moonbean Coffee, Portland · Friday, 9 PM", with "Example"
  in medium-weight `ink`. Then two pages by two usernames from the same
  evening, side by side from `sm` and split by a 1px `rule` gutter
  (`divide-x`), stacked on phones with a `rule` hairline between them. Each
  page carries its writer's name as a running head (`text-sm` `ink-muted`)
  over a few lines of prose in Literata at `text-prose`. A phrase on the
  first page is the character, underlined 2px in `accent` with no fill.
  Under the pages, two `text-sm` lines, each with a 16px `ink-muted` icon:
  the other writer claiming it ("that was me"), and the linked stories
  becoming the canon of the night.
- **How it works.** Under the example, the `h2` "How Perspectiv works" over
  four numbered lines (an `<ol>`, `list-decimal`, no icons): every story is
  pinned to a place and a day; everyone who was there writes their own
  side; spot yourself in someone else's story and claim the character;
  linked stories become a lore, and the lore the people who were there
  favour most becomes canon. A "Read more" link under the list opens the
  guide, `/how-it-works`.
- **Where people are writing.** Under how it works (for a member, under
  the search), up to six rows from the
  most remembered days anywhere, one per place, in a list split by
  `divide-rule` with a `rule` above and below, set as the place page's day
  rows (`ArcItem`). Each row is one link, at least 44px tall, to the place
  page: the place name and its city in Literata `text-lg`, `accent`,
  underlined on hover; the day and its title on a `text-sm` `ink-muted`
  line; the opening, quoted, in Literata clamped to two lines (a two-line
  preview); then a `text-sm` line with "Canon" and a crown (with the
  lore's name when it has one, as on the place page) and the story count.
  A row never links straight to a story, so the way in stays place first.
  The section is left out when no place has a story.
- **Places you follow** (signed in, when there are any) sits above "Where
  people are writing": one pill per place with a bell icon, linking to it.
  A place with stories published at it or below in the last seven days
  carries the count after its name ("Springfield · 3 new", the count in
  bold accent), and those places come first, most new first. Such a pill
  opens the place's Newest tab (`/places/:id?tab=newest`), where the new
  days are marked; a pill with nothing new opens the bare place page. When any are
  new, a line under the heading totals them ("5 new stories this week");
  counts stop at 100 and show as "100+". The reader's own stories never
  count as new.
- **Writers you follow** (signed in) sits under "Places you follow", inside
  the same section, under its own `h2`: one pill per followed writer who
  published a readable story in the last seven days, most new first, each
  a lucide `PenLine`, the username and "· 2 new" (the count in bold
  accent, "100+" at the cap), linking to the writer's profile
  (`/users/<username>`). Writers with nothing new are left out, and the
  heading with them when none has anything new. The pills are the place
  pills' shape: 44px tall, `rule` border, `accent` on hover.
- **Returning members** do not see the title page, the example or how it
  works. The page leads with a compact "Welcome back" block: the page
  heading "Welcome back, *username*" at the page-title size, then one line
  per thing waiting, each a count in bold and a link ("2 unread
  notifications · Open your inbox", "1 request waiting on you · Review
  requests"), or "Nothing is waiting on you." when both are zero. "Your
  stories" follows it, then the search field, then "Places you
  follow", "Where people are writing", "Browse by kind of place" and
  "Browse by state".
- **New members.** A signed-in member with no published story is not
  "back": the heading reads "Welcome, *username*" at the same size, and
  the waiting lines are the same, except that "Nothing is waiting on you."
  is left out: a member who has not written yet has nothing to wait on,
  so the line would say nothing. Under them sits one call to action, the
  primary button "Write about a place you were at" (to `/compose`), and
  under that a short explainer in a bordered `surface` box: the `h2` "How a
  day works" over four numbered lines, the arc page's explainer without its
  counts. No link points at the box; it sits right under the button.
  1. Every story is pinned to a place and a day, or one hour of it.
  2. Everyone who was there writes their own side.
  3. If someone else's story describes you, claim that character; your
     two stories link, and linked stories make a lore.
  4. The lore the day's writers favour most becomes its canon.
  A "Read more" link under the list opens the guide, `/how-it-works`.
  While the member's published count is still loading the heading reads
  "Welcome, *username*" and neither block shows; "Welcome back" replaces
  it once a published story is found.
- **Your stories.** One row per draft or story, newest edit first.
  A row's title is the day's title; a draft that joins an existing day
  (or hour) takes that day's title, a draft that would start a day takes
  the title the writer gave it, and a draft with neither takes its first
  words (six at most, "…" when cut), else "Untitled draft". "Untitled"
  never appears alone. A draft whose day or hour already holds the
  member's published story can never be published: its second line reads
  "Draft · You already published for this day" ("this hour" for an hour)
  in `warning` ink, and a "Delete" text button sits at the row's end,
  44px tall, labelled "Delete draft *title*". One press deletes it, the
  list is read again, and focus moves to the section heading. Nothing is
  deleted without that press. Every other draft row ends with a
  "Continue" text link where a published row has "Edit": the same
  `text-sm` `accent` underlined word, 44px tall, labelled "Continue
  *title*", opening the draft in the composer (the row's title links there
  too).
- **Browse by kind of place** sits above "Browse by state" for everyone: the
  heading "Browse by kind of place", then one link per category that has at
  least one day, in the order of the place page's category chips, each its
  short label ("Sports", "Food & drink") linking to `/best/<slug>`. The
  links are a run-in list, like a book's index: `accent` underlined words
  in a wrapping line, each 44px tall, separated by `ink-muted` middots
  (`aria-hidden`). The section is left out when no category has a day, and
  while the list loads or fails, rather than showing an error on the front
  door.
- "Browse by state" (the heading says just that) closes the page for
  everyone, a run-in list of the states like the kinds.
- Nothing animates, and everything keeps the 16px gutter with no sideways
  scroll at 375px.

## Voice

Interface copy is plain and short: "Publish", "Claim character", "Vote to
remove". Canon is chosen by the people who wrote about the day, never by
"readers": only they can upvote or post in a lore's thread (A-012). Copy says
so ("the lore the people who were there favour most"), and a reader who
cannot vote or post is told who can instead of being urged to write. Book words appear where they are true (part, page, chapter of an arc)
and nowhere else.

"Arc" is a word for the code, never for readers. Every string a reader
sees or hears (text, labels, accessible names, page titles) says "day"
for a day arc and "hour" for an hour arc (`arcSpan`): "Back to the day",
"Day title", "Day not found", "Each hour has its own canon". Where the
kind is not yet known (loading, not found) it says "day". URLs and code
identifiers keep `arc`.

"Perspective" is likewise a word for the code. One person's written account
MUST be called a "story" in every string a reader or writer sees or hears:
page and tab titles, headings, buttons, accessible names, status lines,
empty states, notification sentences and settings labels, email subjects
and bodies, and the server error messages a user can see. "Perspectiv" is
the product name and stays. A tagline MAY use "perspective" as a nod to
the name. Code identifiers, proto and RPC names, enum values, URLs
(`/perspectives/:id`), database columns, query keys and validation errors
that name a wire field (`perspective_id is required`) keep `perspective`.
New copy MUST use these fixed phrasings:

- The composer and place picker heading and tab title: "Write a story";
  a published one being edited: "Edit your story". The day page action:
  "Write your story".
- The front page section: "Your stories".
- A story by name, as a heading, link or accessible name: "otter's story"
  ("Save otter's story", "Report otter's story"); with no name, "this
  story".
- Counts: "1 story", "14 stories". A day's count of stories in its lores
  follows the story count as "14 stories · 12 linked", and is left out
  at zero; "in lore" is not used as a count.
- Not found: "Story not found".
- A request: "Accepting one links both stories into the same lore."
- The notification setting: "A new story on your day"; a claim:
  "otter claimed a character in your story".
