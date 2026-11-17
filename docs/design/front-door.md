# Folio: the front door

How the landing page, the guide and the legal pages look. This page is part of
[Folio](../design-system.md); the principles, tokens, components and voice
there apply to everything here.

## Legal and trust pages

`/terms`, `/privacy` and `/guidelines` are public reading pages: a title, a
"Last updated" line, prose at the reading measure with plain `h2` sections
and bullet lists, and a closing row of links to the other two. The footer
links all three on every page, after "How it works" and before
"Appearance", and the sign-up terms box links them before the
age-and-agreement checkbox, so a keyboard user meets them first.

## How Perspectiv works (the guide)

`/how-it-works` is a public reading page that explains the whole product
to a first-time visitor. Newcomers could not work out day, story, lore,
canon, claims, voting out or hours from four-line summaries, so this page
says each rule exactly, once, in the book's voice, and follows one worked example the whole way through.

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

The app should start out looking like a novel, so the front door is a book's title page set in Folio, not a product
hero: the same paper, type, hairlines and quiet as every reading page, with
no exceptions to Folio's principles. There is no feed of posts: the way in is a place. The page shows one example, as two
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
