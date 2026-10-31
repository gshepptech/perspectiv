# Backlog

What is left before a public beta, in the order it is being worked. An item
moves to Done in the same commit as the work, with what was verified.

## Doing

Nothing in progress.

## Measured and set aside

- From the second load test (HEAD e8a5bd2, 20/50/100 readers): turning
  off parallel query for the API pool (two A/B runs disagreed, so no gain
  was shown); forcing fresh plans only for the wide listings rather than
  the whole pool (saves Postgres CPU at 4 to 6 cores busy, not a limit
  yet, and adds a second query path); running the town lookup alongside
  the plain search (cuts its latency but holds two connections). Raising
  DB_MAX_CONNS past 25 bought nothing: Postgres ran queries on 5 to 8
  connections, and the rest was round trips. Revisit with production
  traffic.

## Out of scope for v1

- Full-text search over stories: the spec leaves it out (A-057); place
  search and character search are the ways in.

## Waiting on a decision or an event

- **A voice for readers who were not there.** Today a reader can react
  and save, but only the day's writers upvote, claim and post in a lore's
  thread (A-012, A-072). A walkthrough found this lands as a closed door
  for Reddit users. Options: a reader-comment area under each story, kept
  apart from the lore thread; or a "readers' favourite" upvote that never
  counts toward canon. Needs the founder: it changes who may speak.
- **An author pointing at someone they wrote about.** Only the person who
  was described can claim ("that was me"). An author-side "Was this you?"
  suggestion, which the other writer accepts as a claim, would link more
  stories; it changes who starts a claim (FR on claims). Needs the founder.
- **Hour and day stories about the same moment.** Hour arcs keep their own
  lore (A-015, A-019), so a day story and an hour story describing each
  other can never form a lore. The composer now steers writers to the whole
  day; letting a claim cross between a day and its hours would close the
  gap. Needs the founder to revisit A-015/A-019.

- **Legal review and a contact address.** The Terms, Privacy and
  Community guidelines pages describe what the code does today; they need a
  lawyer's review and a real contact address from the owner before launch.

## Done

- Release check on k3s at dc37f9e: deploy/k3s/e2e.sh passes from a clean
  checkout in 325 s, image build included. The migration Job applied 1 to
  36; api and worker came Ready; /healthz answered through the ingress over
  TLS; Postgres ran with shared_buffers 512MB, effective_cache_size 1536MB
  and max_connections 100 from the chart's -c args; auto_explain showed
  jit = 'off' on every app statement; /robots.txt and /sitemap.xml named
  https://<host>. With the dev data restored by the runbook's command (638k
  places) and goose taken down to 27, an upgrade's Job ran 28 to 36 in 11 s
  (arcs held 26 rows, so 31 and 33 were not timed at beta scale). The
  backup CronJob wrote a 49 MB archive; after truncating places, the
  runbook restore brought back the same row counts in all 38 tables in
  28 s. An api rollout answered 2 of 327 edge requests with 502: the old
  pod stopped listening on SIGTERM while ingress-nginx still routed to it.
  The api now sleeps 5 s in preStop with a 30 s grace period; two rollouts
  then answered 713 of 713. Verified by TestAPIServesUntilTheIngressDropsIt
  and the k3s run.
- A day page reads its own canon in the same batch as its hours' canons.
  GetArc on a day read the day's canon, members and encounters (three
  statements) before the hours, then the hours' canons, members and
  encounters (three more); the day's canon now joins the hours' batch
  (ListLoresByID, ListLorePerspectives, ListLoreEncounters once each). An
  hour page still reads its canon on its own. Statements per call, BEGIN
  and ROLLBACK included (querycount_integration_test.go): GetArc on a day
  with a canon and an hour canon 15 to 12 signed out and 17 to 14 signed
  in; a day with no canon, or with a canon and no hour canons, sends what
  it did. The response is unchanged: a temporary harness compared the old
  and new GetArc on 374 calls (17 days and hours in five shapes, signed out
  and in, page sizes 0 to 10, deleted and hidden members, named lores,
  votes on day and hour canons) and the deterministic proto bytes matched;
  shifting the hours' canons by one failed it.
- The app's pool runs with JIT off: once row estimates grew (beta-scale
  arcs loaded), Postgres JIT-compiled each short search for 20 to 30 ms,
  longer than it ran. Verified by TestOpenPoolSessionSettings.
- Place reads send fewer statements, and short searches read a few
  hundred names instead of tens of thousands. GetVisiblePlace returns the
  place's ancestors from the walk that checks it is visible, so every read
  of one place drops ListAncestors (statements per call, pinned by
  querycount_integration_test.go): GetPlace 2 to 1 for a town, 3 to 2 for
  a store signed out, 4 to 3 signed in; GetEpic for a place 3 to 2 on each
  tab; a search under a parent 4 to 3; the parent check in ListChildren,
  StoriedChildren, FollowPlace, SearchCharacters and CreatePlace, and each
  place ListFollowing reads, one fewer. A one- or two-letter search (at
  most one parent) now reads exact names, then states and cities, then
  towns, through migration 36's places_name_locality_idx (kind, name, with
  the path, 18 MB), and malls and stores only when those leave fewer than
  100 candidates; the substring step's gate moved into its LIMIT, so a
  skipped step no longer starts parallel workers. EXPLAIN ANALYZE on the
  dev database (638k places), median of 15 warm runs interleaved with the
  old query: "a" 35,627 names read to 265, 12.5 to 1.6 ms; "sa" 10,420 to
  146, 4.3 to 1.3 ms; "th" 21,121 to 814, 7.6 to 1.4 ms; "a" under
  Florida 160 to 180 ms to 2.1 ms; "th" under Miami 7.8 ms both (two towns,
  so the old malls-and-stores read still runs); "park" 1.6 to 1.5 ms and
  "starbucks tampa" 1.3 to 1.4 ms (plain) and 2.3 to 2.5 ms (under Tampa),
  the same plans. Results are unchanged: the old and new SearchPlaces SQL
  returned identical rows for 1,301 searches on the dev database (every
  one- and two-letter prefix, each kind, under each state and 40 cities,
  under Tampa's three localities, wildcards), and
  TestSearchPlacesBroadFirst compares the two ways row for row on a
  mixed-kind fixture at limits 1 to 100 (two mutations of the new steps
  each fail it). Migration 36 Down run on the dev database.

- A day page's statements no longer grow with its hours. GetArc read each
  hour's canon, members, encounters and story count one hour at a time (1
  to 4 statements an hour); it now reads every hour on the page together:
  ListHourArcs carries each hour's readable story count, and the hours'
  canons, their members and their encounters are one array query each
  (ListLoresByID, ListLorePerspectives, ListLoreEncounters).
  GetArcLore and CountVisiblePerspectives are gone. Statements per call,
  BEGIN and ROLLBACK included (querycount_integration_test.go): GetArc on
  a day with two hours 17 to 15 signed out and 19 to 17 signed in; the
  hours section 9 to 7; a day of five hours with a canon each sends 10,
  the same as a day of one. The response is unchanged: a temporary harness
  compared the old and new GetArc on 64 calls (days and hours, signed out
  and in, page sizes 0 to 10, hidden and deleted members, named lores) and
  the marshalled bytes matched; a new test checks five hours' canons,
  members, encounters and counts each land on their own hour. EXPLAIN
  ANALYZE on generated data in the dev database (120k arcs, 300k stories,
  100k claims, 50k users, rolled back): every batched query is
  index-backed (arcs_parent_arc_id_hour_idx, perspectives_arc_author_key,
  lores_pkey, lore_members_pkey, claims_active_character_key), each under
  4 ms cold; no migration was needed.

- Security review of commit e8a5bd2, six findings fixed. (1) A character
  label, and a new day's title (the only other writer text an email
  reads), is put on one line when a draft is saved and when a story is
  published or edited: every run of whitespace, line breaks included,
  becomes one space, and a label or title holding a control, zero-width or
  bidi override or isolate character (U+200B to U+200F, U+202A to U+202E,
  U+2066 to U+2069, U+FEFF) is InvalidArgument ("has an invisible or
  control character; remove it"). The email also flattens labels and
  titles stored before this (apiutil.PlainLine). Lore names, thread posts
  and place names were checked and never reach an email; usernames are
  ASCII letters, digits and underscore. (2) A new story's email no longer
  carries its labels: the email worker reads them as each attempt sends,
  so a story hidden, deleted or voted out while the job waits names
  nobody. (3) Place search refuses a query over 200 characters, before any
  read; the three web search boxes stop at 200. (4) The twin lookup reads
  the places in a 2 km box (migration 35: places_point_idx, a GiST index
  on the point, and arcs_place_path_idx with fastupdate off so probes never
  scan a pending list) instead of every arc in the state. EXPLAIN ANALYZE
  on generated beta-scale data (90k days, 225k stories, rolled back): a
  storyless store in downtown Miami (5,300 places within 2 km) went from
  1.1 s warm (33 s with 90k new days in the GIN pending list) to 25 to
  43 ms warm, and one in a small town from 0.8 s to 0.1 ms; the same 951
  candidates inside 2 km, the 963 left out all same-parent places beyond
  2 km that the rule rejects anyway. (5) The twin lookup and the
  locality ranking no longer count a story under a hidden place. (6) The
  unclaimed-characters read (inbox and email) follows ListOpenCharacters:
  no story voted out of its lore, by a deleted account or at a hidden
  place. Migration 35 Down run on the dev database. Verified by new tests
  that fail on the code before the fix: TestCharacterLabelsReadAsOneLine
  and TestDraftTitleReadsAsOneLine (save and publish), TestCheckCharacters,
  TestCheckTitle, TestCastPhrase, TestEmailWorkerRendersTemplates (labels
  in a job are not mailed; a two-line title), TestNewStoryNamesUnclaimedCharacters
  (hidden, deleted, voted-out, hidden-place and deleted-account stories
  after delivery, in the email and the inbox), TestSearchRefusesLongQueries,
  TestSearchPlaces, TestSearchLocalityIgnoresHiddenStories,
  TestGetPlaceTwin (a story under a hidden place below; the same-parent,
  200 m and 2 km cases still pass), TestHotQueriesAreIndexBacked and
  TestArcPlacePathIndexWritesThrough; plus TestOneLine, TestHasHiddenRune,
  TestPlainLine and the web suite's maxlength checks. make check and
  make e2e (18 passed) pass.

- Fewer round trips on the day page's reads, from the second load test
  (GetArc sent 10.3 statements a call and took 16% of database time;
  BEGIN and ROLLBACK of read-only snapshots another 10 to 11%). GetArcRow
  now returns the place chain from its own walk up the places, so the
  PlaceChain query is gone; the hidden-place rule (the place and seven
  above it) and the chain's order and fields are unchanged. A signed-out
  ListArcSection page of tales or unlinked perspectives, and
  ListOpenCharacters, read through the pool without a snapshot: each is
  the arc row and one list that reads nothing from it. Sublore and hour
  pages, and any signed-in page, keep the snapshot, because a lore's
  member count against its members, an hour's canon against its story
  count, or an upvote mark against its count could disagree. Statements
  a call, BEGIN and ROLLBACK included, on the TestArcReads_QueryCount
  fixture (a day with a canon, a sublore, a tale, an unlinked story and
  two hours), before and after: GetArc day 18 to 17 signed out and 20 to
  19 signed in, GetArc hour 10 to 9; ListArcSection tales or unlinked
  signed out 5 to 2, tales signed in 6 to 5, sublores 7 to 6, hours 10 to
  9; ListOpenCharacters 5 to 2. That test pins the new counts with a
  counting tracer on its pool.
- A first-time visitor's walkthrough of finding a place. Search now reads
  a name with the city or town it is in ("starbucks tampa", "tampa
  starbucks", "starbucks in tampa", "starbucks, tampa, fl"; St./Saint,
  Ft./Fort and Mt./Mount spelled either way), searches the name under each
  matching visible city or town, best first (one with stories, then a
  city before a town, then the larger), and keeps the plain search: its
  name starts lead, its substring matches follow. A capped search says so
  (`more` on SearchPlacesResponse) and every search list shows "More
  places match: add a town to narrow it". Search is in the header on every
  page (a dialog that "/" opens outside text fields); the composer's
  picker shows what and where each place is and its story count, in the
  server's storied-first order; "Your profile" heads the account menu; the
  Inbox and Requests badges no longer cover their words. Migration 34
  replaces places_name_prefix_idx with places_name_rank_idx, a covering
  index the prefix ranking reads without the heap. docs/design-system.md
  updated first. Verified on the dev database (640k places), EXPLAIN
  ANALYZE warm: "starbucks tampa" 1.2 ms plain, 0.2 ms localities, 2.3 ms
  under Tampa; "valley mall" 1.8, 1.0 and 0.6 ms; "starbucks" 2.7 ms; "ta"
  3.2 ms and "th" 7 to 10 ms (55 ms and 87 ms before); whole requests 10 to
  30 ms warm. Integration tests cover every form, the locality ranking, the
  plain search first and as the fallback, hidden towns, the kind and
  parent filters and `more`; unit tests cover the splits, spellings and
  merge; a Chromium test measures the badges at 375 and 1280 pixels (fails
  on the old placement) and the dialog's fit; place-search.spec.ts drives
  the header search end to end. `make check` passes.
- A new writer's walkthrough: drafts, the first welcome, who is in a new
  story, and judging a request. Drafts no longer pile up as "Untitled":
  a draft is titled by the day it joins (the server now fills a draft's
  `arc_title` from the arc for its place, day and hour), else the title
  it gave, else its first six words, else "Untitled draft", on the front
  page and in every story row; opening the composer for a place, day and
  hour where the writer has a draft resumes it with a note and names it
  in the address; a draft the writer's own published story holds the day
  for (new `superseded_by_id`, hidden stories included, deleted ones not)
  reads "Draft · You already published for this day" (or hour) with a
  one-press Delete, and nothing is deleted on its own. A member with no
  published story is welcomed ("Welcome, *name*") with one call to
  action, "Write about a place you were at", and a "How a day works"
  explainer written into the design text; "Welcome back" stays for
  members who have published. A new story's inbox row and email name up
  to three characters nobody has claimed (no PENDING or ACCEPTED claim),
  then "and N more", read from the database at delivery (email) and at
  listing (inbox), never from the job, and left out once the story is
  deleted or hidden. An incoming request quotes about 240 characters of
  the claimant's story, cut at a word, with "Read their story", and says
  "Their story can no longer be read." when it is deleted, hidden or at a
  hidden place. docs/design-system.md updated first. Verified by
  integration tests (a draft's day title and superseding story through
  Get and ListMine for no day, someone else's day, the writer's own day,
  an hour, a deleted and a hidden story, a hidden place; the unclaimed
  characters in the email job and the inbox, with pending, accepted and
  rejected claims, a claim, delete and hide after delivery, and other
  kinds naming nobody; the claimant excerpt for a short, long, empty,
  deleted, hidden and hidden-place story in both directions), unit tests
  (email wording, draft titles, the welcome and explainer focus, the
  superseded row and its Delete, the resumed draft, the inbox line, the
  request quote), a new Playwright journey (`e2e/new-writer.spec.ts`,
  with its own fixture place) through Mailpit, and by `make check` (946
  Go unit, 628 web unit, 1870 integration tests, floors met) and `make
  e2e` (18 passed). The composer, inbox, notification settings, Requests
  page and notification emails now say "day" where they said "arc"
  ("Title of the day", "one story per day", "your day"). The story page (`web/src/routes/read`) still titles a
  draft with neither a day nor a title "Untitled"; it can use
  `storyTitle` from `web/src/components/reader/storyTitle.ts`.
- Reader walkthrough fixes: the day page, duplicate places, following,
  "arc" in readers' words, and two visual slips. A reader who may not vote
  on a day (signed out, or with no story there) now sees each row's count
  with no button, and one note above the stories with the story page's
  words ("Only people who wrote about this day can upvote its stories…",
  "Were you there? Write your side", and signed out "Already wrote yours?
  Sign in"); the story page's signed-out upvote note says the same, and
  both say "hour" on an hour arc. GetPlace returns a `twin` for a
  storyless store or mall that is a second listing of a storied one
  (placectl dedupe's name key and radius, or the same parent within 2 km),
  and the empty place page says "People write about this place at
  <twin>." A followed place with new stories opens on its Newest tab,
  where days with a story from the last seven days carry a "New" label
  (EpicArc `new_this_week`). Readers see "day" or "hour" where they saw
  "arc" (back link, hour timeline, not-found and loading states, the
  place page's "Days" list), the Voice section says so, and an unnamed
  lore's page is titled "Unnamed lore" instead of its day's title. The
  author's Invite/Edit/Delete row and the draft banner now line up with
  the story's text column. Verified by integration tests on the twin
  rules (found under one parent 780 m away, under another parent within
  200 m, a venue within 2 km, most stories wins; not found when hidden,
  under a hidden parent, storyless, its only story hidden, too far under
  either parent, a different name, the place having its own story, a
  city) and on `new_this_week` (a hidden recent story does not count), by
  unit tests for the inert rail and the note, the twin line, the New
  label on Newest only, the Newest link from a followed place and the
  unnamed lore title, against the dev data ("Raymond James Stadium,
  Tampa" points to the storied "Raymond James Stadium"), and by `make
  check` and `make e2e`. The composer, inbox, claims, notification labels,
  emails and every server error a reader can see now say "day" too
  ("you already have a story about this day").
- Accessibility and phone-width pass over the newest screens (the lore
  thread, Follow and "Writers you follow", Save and /saved, /appearance,
  /best/<slug> and the front page's kinds row, "More from this day", the
  report queue's "What it said when reported"), at 375 and 1280 pixels, in
  both themes and at Larger story text. axe found no WCAG 2.1 A/AA
  violations, nothing scrolled sideways and nothing moved under reduced
  motion. Found and fixed: a reply written and then deleted (or edited)
  stayed on screen as its stale local copy; a new top-level post left focus
  on Post and was not shown until the re-read; vote counts, the thread's
  Top/New order and the Best page's order changed silently; Appearance said
  "Saved on this device." only after the first choice and hid the
  heading's focus outline; Unfollow in Settings dropped focus to the page
  when its row left; the follower count changed silently; thread byline
  author links (20px), Unfollow (36px), the queue's Resolve (36px) and
  target link (20px), and the Best page's "Search for a place" links were
  under 44px; the queue's disclosure had no open/closed marker, a box 22
  lines tall instead of 20, and a scrolling box the keyboard could not
  reach. docs/design-system.md updated first. Verified by new unit tests
  (thread post focus, reply edit and delete, vote and sort status,
  unfollow focus, follower count live region, Appearance re-announce,
  report box in the tab order, Best order status), a 375-pixel target-size
  check of these screens in phoneWidth.test.ts (fails on the old code),
  and a11y.spec.ts, which now scans the thread, /saved, /appearance, a
  profile with Follow and the open report copy in both themes. Seen and
  left for another pass, outside the screens in scope: the lore page's
  "Name this lore" (36px), its story-list author links and cast-tag links
  (20px).

- The front page's everywhere listings read an index in rank and newest
  order (migration 33) instead of sorting every day: 55 ms warm and 368 ms
  cold at beta scale (90k days) become 0.5 ms. Verified by EXPLAIN ANALYZE
  on generated beta-scale data (rolled back) and two new no-sort cases in
  TestHotQueriesAreIndexBacked, which fail without the index.
- Canon by category: GetEpic takes a PlaceCategory, so RANKED and NEWEST
  list only the days whose own place is of that kind, everywhere or under a
  place, and TALES filters the same way (an unknown value is
  InvalidArgument); ListStoriedCategories (public) names the kinds with a
  readable day. The public page /best/<slug> ("The best days at sports and
  recreation places", Ranked and Newest) is reached from a "Browse by kind
  of place" row on the front page and from the category word in a place's
  header, and the sitemap gains a "pages" file listing it per kind. The
  slug is the enum name in lowercase with hyphens, derived once in Go
  (place.CategorySlug) and once in the web app (categorySlug). An arc now
  carries its place's category (migration 31, kept by triggers on arc
  insert or move and on a place's re-categorisation, Down run on the dev
  database) with ranked and newest indexes per category: at beta scale
  (90k days, 225k stories, 638k places) the sports listing went from 95 ms
  to 1.6 ms and the food-and-drink listing under Florida from 69 ms to
  1.3 ms, and the category list takes 0.3 ms (13 index probes). Verified by
  TestGetEpicCategory (everywhere, under a state and a city, hidden places,
  one-at-a-time paging for every case, tales, an unknown category),
  TestGetEpicCategoryFollowsReclassification, TestListStoriedCategories,
  TestArcPlaceCategory, TestHotQueriesAreIndexBacked, TestCategorySlug,
  TestPreview's sitemap cases, TestRobotsAndSitemap,
  TestServicesExposeExactlyTheContractRPCs, TestRoutesCoverEveryProcedure,
  the web suite (BestPage.test.tsx, LandingPage.test.tsx, PlacePage.test.tsx,
  format.test.ts, router.test.tsx, the phone-width check) and
  e2e/find-yourself.spec.ts (front page to the best days at shops to the
  day) with /best/shopping in the axe scan. The front page's own ranked
  listing (no category) still sorts every day, about 95 ms at beta scale;
  a global ranked index would fix it and is not part of this change.
- Security review of the follow, save, thread, sitemap and appearance work
  (no high findings; all four lower ones fixed): a report keeps what its
  target said when it was filed (migration 32; a post, a story's parts and
  characters, a name or a profile), shown to moderators under "What it said
  when reported", so editing a reported post to "ok" before review no
  longer hides it; a burst of thread replies is one email an hour per lore
  (every reply still reaches the inbox); notification email goes only to a
  confirmed address, and follower counts leave out unconfirmed and
  suspended accounts; and each sitemap file is built at most once per 30
  minutes. Verified by TestReportKeepsWhatWasReported,
  TestNotificationEmailLimits, TestFollowerCountLeavesOutUnconfirmedAndSuspended,
  TestSitemapConcurrentRequestsBuildOnce, TestSitemapFailedBuildIsNotKept and
  ReportsPage.test.tsx.
- Search engines: /robots.txt keeps crawlers off the API, account, writing,
  admin and one-time-link pages and names /sitemap.xml, an index of three
  sitemaps (places, days, stories) built from the same readable-only rules
  as the pages: nothing hidden, under a hidden place, deleted or a draft.
  Each lists the 50,000 most recently active pages, the protocol's per-file
  limit; past that, the files need paging (a follow-up, not needed at beta
  scale). Verified by TestRobotsAndSitemap, TestRobotsWithoutSitemap and
  TestPreview's sitemap cases; on the dev data each file builds in about
  2 ms.
- Lore threads with replies, upvotes, sort, edit and delete: a post is
  top-level or a reply under one, two levels deep; a reply to a reply sits
  under the same post and shows "Replying to @name". Replies read oldest
  first, three inline and the rest behind "Show N more replies"
  (ListLorePostReplies). The people who may post (A-012) upvote a post once,
  never their own and not when its author blocked them (VoteLorePost, on
  the vote limiter). ListLorePosts sorts TOP (score, then oldest; the
  default) or NEW (newest first), keyset-paged for both. Authors edit
  ("edited") and delete their own posts; a deleted post with replies stays
  as "[deleted]" above them. Hiding a top-level post hides its replies and
  makes them unreportable; restoring brings them back. A reply notifies the
  answered post's author (THREAD_REPLY: inbox, email, preference), never
  the replier and never across a block either way. The export lists each
  post's place in its thread, its edit time and the member's post upvotes.
  Reacts stay on stories (A-033). Migration 30, its Down run on the dev
  database. Verified by TestThreadReplies, TestVoteLorePost,
  TestLorePostSort, TestEditLorePost, TestDeleteLorePost,
  TestThreadHideAndRestore, TestLoreThread, TestFanoutThreadReply,
  TestHideAndRestoreThreadPostWithReplies, TestExportMyDataThread,
  TestMigrationsRollBack, TestEnumValues,
  TestServicesExposeExactlyTheContractRPCs, TestRoutePolicies,
  TestEmailWorkerRendersTemplates, the web suite (LoreThread.test.tsx,
  LorePage.test.tsx, inbox, settings and phone-width tests) and
  e2e/lore-journey.spec.ts (a post, a reply that reaches its author's
  inbox, two upvotes, and Top and New).
- Appearance, for every reader: /appearance (linked from the footer and
  Settings) picks the theme (match the device, Paper or Lamplight) and the
  story text size (18, 20 or 22px), kept on the device and applied before
  the first paint by public/appearance.js. The palette is now one
  light-dark() token set, so the two themes cannot drift apart. Kept per
  device rather than per account on purpose: it works signed out, and a
  phone and a desktop often want different sizes. Verified by
  tokens.test.ts (both themes still meet WCAG 2.1 AA), appearance.test.tsx
  and e2e/appearance.spec.ts (the choice is on the root before the app
  loads, survives a reload and sets theme-color).
- Cast tags link the claimant's own story from the server: each character
  carries claimed_by_story_id while that story is readable, so the tag no
  longer depends on the story being on the day's first page, and the
  client-side lookup is gone. Verified by TestCharacterClaimedBy (accepted,
  pending, hidden story, deleted account) and the reader and lore tests.
- Day listings name their canon: a place page row and a front-page card
  read "Canon: *The long queue*" once the canon lore's members name it, and
  plain "Canon" otherwise or after a moderator clears the name. Verified by
  TestGetEpicRanked and PlacePage.test.tsx.
- Save a story to read later: a Save toggle (Bookmark, `aria-pressed`,
  "Save"/"Saved") in a story's action bar, a signed-in /saved page titled
  "Saved stories" with profile-style rows that name the writer, Show more
  and an empty state, and "Saved" first in the account menu; a signed-out
  reader who presses Save is sent to sign in and back. SavePerspective is
  NotFound for a story the caller cannot read (draft, deleted, hidden, at
  or below a hidden place), for an unsave too; ListSaved pages newest save
  first and leaves out a saved story that stops being readable, keeping
  the save so the story returns once restored; Get's perspective carries
  `saved` for the caller. Deleting an account removes its saves (the
  account is soft-deleted, so the FK cascade alone would not); the data
  export lists saved story ids with when they were saved. Migration 29.
  Verified by TestSavePerspective, TestSavePerspectiveNotFoundWhenUnreadable,
  TestListSaved, TestSavedPerspectivesCascade, TestHotQueriesAreIndexBacked,
  TestDeleteAccount, TestExportMyData, TestMigrationsRollBack, the web
  suite (actions.test.tsx save cases, SavedPage.test.tsx, router.test.tsx)
  and e2e/save-story.spec.ts.
- Follow a writer: a Follow toggle and follower count on a profile, a
  "Writers you follow" list in Settings with Unfollow, and a new inbox and
  email kind (NEW_STORY_BY_FOLLOWED_WRITER) for each new story by a followed
  writer, sent once per story per person (a place follower or a writer in
  the arc hears of it that way instead). Blocking removes follows both ways
  in the block's transaction and refuses a follow across a block; deleting
  an account removes its follows; the data export lists the writers
  followed. Migration 28. Verified by TestFollowUser,
  TestFollowUserRefusals, TestBlockRemovesFollows, TestListFollowedWriters,
  TestExportMyData, TestFanoutNewStoryByFollowedWriter,
  TestPublishSideEffects, TestEmailWorkerRendersTemplates and the web suite
  (FollowWriter.test.tsx, settings and inbox tests).
- Load test of the reader journey (20, 50 and 100 closed-loop readers on
  the dev data): the pgx pool sat at its default of one connection per CPU,
  full from 20 readers, so throughput fell as load rose. The pool is now
  DB_MAX_CONNS (default 25, chart value app.dbMaxConns) and /metrics
  exports its state (db_pool_acquired_conns, db_pool_empty_acquires_total,
  db_pool_acquire_wait_seconds_total and more); the chart's Postgres gets
  512MB of shared buffers so the places table stays in memory; and place
  search reads prefix matches first and runs the substring scan only when
  fewer than 100 match, so "park" takes 7 ms warm instead of 110 to 675 ms.
  Not changed, on purpose: caching the live-session check would let a
  revoked session keep working for the cache window (security state is read
  from PostgreSQL every time), and GetArc keeps its single read-only
  transaction so a page never mixes two moments. Verified by
  TestRegisterPoolExportsPoolState, TestLoadConfig*, TestPostgres18OnPersistentVolume,
  TestSearchPlaces and EXPLAIN ANALYZE on the dev data (trigram scan never
  executed).
- Reading experience, from a reader walkthrough: a followed place on the
  front page says how many stories it gained this week ("Springfield · 3
  new"), with the busiest first and a total under the heading; a finished
  story ends in "More from this day" (its lore, five more of the day's
  stories, the way back) instead of a dead end; a short story reads whole
  on one page and a long one can; the byline names the story's standing in
  plain words with a link to how the day works; every page titles its tab;
  cast tags open the claimant's own story; profile and place rows show
  each story's opening; and excerpts end at a word with an ellipsis.
  Verified by TestFollowingNewThisWeek, TestClipExcerpt,
  TestExcerptEndsAtAWord and the web suite.

- E2E sign-up budget: each e2e account signs up from its own
  documentation-range address (the browser reaches the server over loopback
  through Vite, so the rightmost X-Forwarded-For is the one counted), so new
  specs can add accounts without exhausting one address's hourly sign-up
  limit. Verified by the e2e suite (14 passed) and the signup counters: one
  per address.
- Place dedupe applied to the dev data: 144 storyless near duplicates
  hidden into their twins, each audited; a second run finds nothing.
- Spec audit (162 FR, AC and OT items: 158 met with tests, none unmet):
  a member who deletes their story in an arc may write there again (the
  one-story rule now ignores deleted stories; a moderator's hide still
  holds the place; migration 27); publishing a day that has not happened
  yet where the place is is refused on the server, not only by the web
  picker; the ingress forwarding settings the per-IP limits rely on are
  pinned by a chart test; AC-009 and FR-045 carry amendments for the later
  decisions that changed them; and the phone-width and axe tests (delegated)
  cover every reading route. Verified by TestPublishAfterDeleteInSameArc,
  TestNotAfterToday, TestIngressNginxValues and migration 27 up-down-up.
- Place data quality in the importer: localities match through St/Saint,
  Ft/Fort, Mt/Mount, case and punctuation (St. Petersburg places were
  dropped as unfiled), and unfiled counts are logged by locality; names
  lose a trailing own city or state and stray punctuation and all-lowercase
  names are title-cased, with a collision keeping the existing name instead
  of failing the import; stadiums, arenas, ballparks and race tracks import
  as sports venues (mall level) and a re-import never demotes a venue; and
  `placectl dedupe` (dry run unless --apply) hides storyless, draftless
  near duplicates (200 m, 2 km for venues) into the best-ranked twin,
  moving children and follows and auditing each. A dry run on a copy of the
  dev data found 144 merges and touched nothing. The missing Tropicana
  Field needs the source extract to confirm. Verified by the importer and
  placectl tests at both tiers.
- An admin merge counted only a duplicate's arcs, so it could hide a place
  a draft was still filed at; it now counts drafts too. Verified by
  TestMergePlaceKeepsTheHierarchy.
- Accessibility and phone pass over waves 6 to 8 (axe-core found no WCAG
  violations in 22 scans at 375 and 1280; these are the manual findings):
  a place page now names a venue by its category (GetPlace never carried
  it, so the header still said "Store"); the composer shows "you already
  have a story here" right under the day, not 1,400px down, and says why
  Publish is held; "Write your side" focuses the heading and shows the
  fixed day and hour as text, not as controls that look editable; plus the
  delegated web fixes for the merge dialog at 375, announcements behind the
  claim dialog, focus after a claim or report, status lines read as
  themselves, 44px targets, unique device sign-out names. Verified by
  TestGetPlace, the composer tests and the web suite.
- Security review fixes (waves 6 to 8): a place merge could file a city
  under its own town (its children moved under themselves) or a town under
  a town, and could merge into a place a hidden ancestor hides; the merge
  now refuses a target inside the duplicate, compares the shallowest child
  level, and requires the target's whole path visible. safeNext refused
  "//" but not "/<tab>/host", which URL parsing turns into "//host"; it now
  refuses tabs, newlines and backslashes anywhere. Search story counts no
  longer count stories under hidden places. Verified by
  TestMergePlaceKeepsTheHierarchy, TestSearchCountsSkipHiddenPlaces and the
  safeNext tests.
- Release check on k3s: the documented production path (deploy/k3s/e2e.sh)
  hung at helm --wait, because local-path binds a volume claim only on its
  first consumer and nothing used the backups claim before the first
  nightly run; the database pod now mounts it read-only, so it binds at
  install. The backup job's pg_dump was refused for its first second while
  the network policy admitted the new pod; backup.sh now waits for
  pg_isready (up to 60 s). With both, the full k3s run passes in 155 s;
  migrations 21 to 26 on 638k places took 113 s against the 900 s deadline.
  Verified by the k3s e2e run, TestBackupCronJob and the backup script test.
- Second-walkthrough web fixes: after "Write your side" the story page says
  "Your story is published"; "That was me" carries the character, which the
  claim dialog opens on, once; a stadium's page header names its category;
  a duplicate report must name the other listing; reacting signed out
  explains instead of redirecting; focus lands on the new status after
  Accept or Reject; invites leave out claimed characters; Requests shows a
  waiting badge. Verified by the web suite (455 tests) and e2e.
- Writing your side and one story per arc: signing up signs the new member
  in (SignUp returns a session like Login), so "Continue where you were"
  needs no second password. "Write your side" fixes the day and hour to the
  story being joined and says why, so the claim can't dead-end on an hour.
  The composer knows which arcs already hold the writer's story
  (FindDayArc's mine_perspective_id) and, before any typing, says so,
  offers "Edit yours" and holds Publish, instead of losing a second story to
  a refused publish. Verified by TestSignUpSignsIn, TestFindDayArcMine and
  the composer and auth tests.
- "This is the same place": members report a place as "Same place as
  another listing" (offered for places only, from the place page and the
  arc page); an admin merges it from the place page. "Were you here?" as a
  claim without a story is not built: claims link stories, and the wave 6
  "Write your side" flow from "Is this you?" covers the need. Verified by
  TestReportDuplicatePlace, the refusal table, migration 26 up-down-up and
  the label tests.
- The first-time user (wave 6, from a newcomer walkthrough): "That was me"
  and upvote explain what a newcomer must do first and offer "Write your
  side", returning to the story with the claim open after publishing; the
  next page survives sign-in, sign-up and email verification; returning
  members get a "Welcome back" home with unread and waiting requests and
  their stories first, and an unread badge on Inbox; phone navigation is
  labelled, "Claims" is "Requests"; the password rule shows before submit;
  inbox rows are one link and mark themselves read; accepting a request
  says what happens next and offers an invite. Admins can make an imported
  store a venue that holds places and merge a storyless duplicate into its
  twin from the place page, both audited (migration 25). Verified by the
  web suite, TestMakeVenue, TestMergePlace and e2e.
- One venue, one place in search: results carry the place's story count
  (capped at 100) and rank places with stories first within each match
  tier, and a store with a category reads as what it is ("Sports in Tampa,
  Florida"). Verified by TestSearchPlaces and the label tests.
- Hour and day stories meet: the composer asks FindDayArc what exists for
  the place that day and says it ("This day is “…”: 2 stories for the whole
  day, 1 story at 13:00") with where the writer's story will go, and shows
  the title field only when nobody has written that day, so a title is no
  longer dropped silently. Claims still link only within one arc: hour arcs
  keep their own lore (spec A-015, A-019), so the composer steers writers to
  the whole day instead. Verified by TestFindDayArc and the composer tests.
- Security review fixes (unsubscribe, audit, retention, metrics): a worker
  no longer starts without JWT_SECRET, since the unsubscribe links it signs
  are checked by the API with a key from it; and a user lookup that finds
  no account is audited too ("looked up by email, no match", never the
  text searched), so probing for addresses shows in the log. Verified by
  the config tests and TestLookupUser.
- Signed-in devices: Settings lists each live session as a browser and
  system ("Firefox on macOS"), marks the one in use, shows when each was
  signed in and last used, and signs any other one out. Only that short
  label is stored, never the User-Agent; the privacy page says so. Another
  account's session reads as not found. Verified by TestSessions,
  TestDeviceLabel, migration 24 up-down-up and the settings tests.
- Undoing moderation: an admin lifts a suspension from the dossier and
  restores a hidden perspective, place or thread post from its hide entry
  in the audit log, each with a required reason on a new audit entry
  (AUDIT_ACTION_LIFT_SUSPENSION, AUDIT_ACTION_RESTORE). A restored story
  queues its arc for a lore recount; claims rejected while it was hidden
  stay rejected. Verified by TestLiftSuspension, TestRestore, migration 23
  up-down-up and the dossier and audit page tests.
- UX fixes from a browser walkthrough of the new screens: the dossier
  re-reads and announces after a resolve; the lookup box follows the
  address; server messages read as sentences; "Show more" moves focus to
  the first new entry across eleven lists; admin and settings pages focus
  their heading on arrival; sign out everywhere lands on a confirmed sign-in
  page; stale success lines clear on edit; report reasons read the same for
  reporter and admin; 44px tap targets for preference checkboxes, dialog
  close and admin name links; clearer unsubscribe, confirm-email, empty
  lookup and wrong-password states. Verified by the web suite (401 tests)
  and e2e.
- Request metrics: every API call is counted by procedure and Connect code
  (`rpc_requests_total`) and timed by procedure
  (`rpc_request_duration_seconds`) from an interceptor outside recovery, so
  panics and refusals count too. `make dev-grafana` provisions a
  "Perspectiv overview" dashboard (calls by code, server error ratio, p95
  by procedure, refusals, job and email failures, product activity), and
  Prometheus now scrapes the port HTTP_ADDR names instead of a fixed
  :8080. Verified by TestMetricsInterceptor and the dashboard and series
  read back from the dev stack.
- Character search on common words: a word of three letters or more is
  first looked up by label through the trigram index, capped at 2,000
  matches; a rarer word is then filtered to the place (570 ms to about
  25 ms at beta scale), and a common one keeps the walk of the place's
  stories. Verified by TestSearchCharacters on both paths.
- Email footers: "Change it in Settings" and "Stop these emails" are
  links in the HTML part rather than bare addresses, and a dossier's
  "Moderator actions" leaves out lookups of the account, which the full
  audit log still lists. Verified by the email render tests and
  TestAuditLog.
- Query performance, part 2: lore groups are found in Go from the arc's
  accepted-claim edges with union-find instead of a recursive walk that
  grew with the square of a group (3.7 s under the arc lock at beta
  scale); places carry a `path` like arcs, kept by trigger, so a search
  inside a state is one index lookup (1.6 s to 37 ms on 638k places). A
  state's storied-children listing measured 93 ms with the hidden-places
  index and is left as is. Verified by TestConnectedGroups,
  TestConnectedGroupsLarge, TestPlacePath, the lore, vote, perspective and
  place suites, and migration 22 up-down-up (the backfill takes about two
  minutes on 638k places).
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
