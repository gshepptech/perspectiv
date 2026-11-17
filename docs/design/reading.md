# Folio: reading

How stories, arcs and lores look: the pages a reader spends their time on.
This page is part of [Folio](../design-system.md); the principles, tokens,
components and voice there apply to everything here.

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
  sense of Flairs and tags in [Folio](../design-system.md): a 16px icon (lucide `UserCheck` once
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

The app is meant to read like a novel, and an entry like a page in a
book. A lore read as a book is that idea made literal: the lore's stories bound in
the lore page's order, one chapter each, read a page at a time. It is a
reading surface, so Folio's principles, measure and motion rules
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
