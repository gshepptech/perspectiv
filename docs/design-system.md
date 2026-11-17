# Folio

Folio is the design system for Perspectiv. Perspectiv reads like a novel, not a
feed: a perspective is a short book, each part is a page, and the chrome around
it stays quiet. Where Folio and component code disagree, Folio wins.

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
  front page's place search (see Place search in [Places](design/places.md),
  which gives its keys): the
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

## App icon

The mark is the header's open book (lucide `BookOpen`) in `paper` on an
`accent` ink-blue square: `web/public/icon.svg` for tabs, PNGs at 180 (Apple
touch), 192 and 512, and a 512 maskable icon with the book inside the safe
zone. `manifest.webmanifest` installs Perspectiv standalone with `paper` as
its background and theme colour, and the page's `theme-color` follows the
light and dark themes.

## Voice

Interface copy is plain and short: "Publish", "Claim character", "Vote to
remove". Canon is chosen by the people who wrote about the day, never by
"readers": only they can upvote or post in a lore's thread. Copy says
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

## Surfaces

Each surface has its own page, built on everything above:

- [Reading](design/reading.md) covers stories, arcs and lores.
- [Places](design/places.md) covers place search and the place page.
- [The composer](design/composer.md) covers writing a story.
- [Accounts and admin](design/accounts.md) covers profiles, the inbox,
  settings, sign-in and the admin pages.
- [The front door](design/front-door.md) covers the landing page, the guide
  and the legal pages.
