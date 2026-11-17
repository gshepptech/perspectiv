# Folio: places

How place search and the place page look, with the page's epic, sidebar,
reports and admin tools. This page is part of [Folio](../design-system.md);
the principles, tokens, components and voice there apply to everything here.

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

## Place page sidebar

- **Were you in someone's story?** First in the sidebar: a search field
  (placeholder "red umbrella…", short enough to show whole in the 16rem
  sidebar) over unclaimed descriptions at the
  place and below. Results quote the description in Literata, name the writer,
  place and day, and link "That was me" to the story with the claim dialog up.
  Under two characters nothing is searched; an empty result says so in a
  `status` line.
- The list of places inside follows it.
