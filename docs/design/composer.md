# Folio: the composer

How the composer looks: choosing the place, the day and hour, and joining a
day someone else wrote about. This page is part of
[Folio](../design-system.md); the principles, tokens, components and voice
there apply to everything here.

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
