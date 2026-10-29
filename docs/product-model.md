# Product model

How people take part in Perspectiv: who can do what, how stories link, and
how an arc, its lores and its canon are shown. This file records decisions;
the code implements them, and where the two disagree the code is the bug.
`docs/design-system.md` owns how things look.

## Signing up and the beta

Reading is always public. Writing needs an account, and while
`SIGNUP_MODE` is `invite` (production's setting during the beta, and the
default when unset) an account needs an access code:

- **A member's invite link.** Every "Invite who was there" link admits up
  to five people, while its sender is in good standing and the story it
  shares is still readable. The beta grows from people who were really at the
  same place, without one link opening it to everyone.
- **A beta code** an admin issues for a launch, a campus or a person: a use
  limit, an optional expiry, and revocable. Accounts it admitted keep access.
- **The waitlist.** Anyone without a code leaves an email and, optionally,
  where they would write about. An admin's invite sends them a single-use code
  that lasts 30 days.

Each account records which code let it in. Turning `SIGNUP_MODE` to `open`
ends the beta without a code change.

## Words

- **Arc**: one place on one day (or one hour of it). Everyone who was there
  writes into the same arc.
- **Perspective**: one person's story in an arc. One per person per arc.
- **Character**: someone a writer describes in their story ("the guy in the
  yellow raincoat"). Up to 10 per perspective.
- **Claim**: "that was me". A writer in the same arc says they are a
  character in someone else's perspective. The character's author accepts
  or rejects it.
- **Lore**: perspectives linked by accepted claims. Two stories that see
  each other are a lore.
- **Canon**: the lore readers favour most, once it has at least four
  members. One per arc. Every other lore is a **contender**.
- **Tale**: a perspective nobody linked to within seven days, or one voted
  out of its lore. It stays readable; it just stands alone.

## Who can do what

| Action | Who |
|---|---|
| Read anything public | Anyone, signed in or not |
| Sign up | Anyone 18 or over while sign-up is open; during the invite-only beta, anyone with a live beta code or a member's invite link |
| Join the waitlist | Anyone, while sign-up is invite-only |
| Issue, revoke and send beta invites | Admins |
| React with an emoji | Anyone signed in |
| Write a perspective | A verified account, once per arc |
| Upvote a perspective | Someone with a perspective in the same arc |
| Claim a character | Someone with a perspective in the same arc, not in their own story, at most **one character per perspective**, and never one they were rejected for |
| Accept or reject a claim | The author of the character's perspective |
| Vote to remove from a lore | A member of the same lore, once the lore has four or more members |
| Post in a lore's thread | A member of the lore, or anyone with a perspective in the arc |
| Name a lore | A member of the lore; the latest name wins and shows who set it. After a moderator clears a name, nobody can rename the lore for 7 days |
| Follow a place | Anyone signed in; a new story at the place or anywhere under it lands in their inbox (and email, if they keep it on) |
| Report a perspective, place, user, thread post or lore name | A verified account; an admin hides the post or perspective, hides the place, suspends the user, or clears the name |

Claiming is never required. A story that links to nobody is still
published and read; it becomes a tale after seven days. Claiming is how a
writer joins a lore, so the app asks for it at the moment it is most
natural: right after publishing into an arc that already has stories (the
story page then lists the day's unclaimed characters under "Is this you?"),
on the arc page's own "Is this you?" list, on a place page's description
search, and on any story in the arc that names characters the reader has
not claimed.

**One character per perspective.** A person can only be one of the people
in someone else's story. Once your claim on a character in a perspective
is pending or accepted, that perspective offers you nothing more to claim.
If your claim is rejected, you can claim a different character there, but
never the same one again.

## What the reader sees

**Character tags.** Once a claim is accepted, the character carries the
claimant's name everywhere it is shown: "the guy in the yellow raincoat ·
otter". The tag links to their profile. A pending claim shows as "claim
pending" to the two people involved and to nobody else.

**Claim states on a story.** Each character shows one of: open (you can
claim it), yours (pending or accepted), someone else's (tagged), or closed
to you (you already are someone in this story). The "Claim a character"
action appears only when at least one character is open to you.

**Badges.** On a perspective card: "In lore" or "Canon". On a profile: how
many canons and lores the person is part of.

## The arc page

The arc page is the day as the people who were there remember it, laid out
like a subreddit's front page: an index of the day's stories, organised by
who found whom. Each story is a compact row (upvote rail, author, genre, the
opening two lines, "Read"); reading, claiming, reacting, voting out and
reporting happen on the story's own page and on its lore page.

1. **Canon, pinned.** The canon lore first, marked "Canon of the day", named
   after its people ("knightsfan_04, paintedknight and 3 others"), with its
   stories in publishing order and one link into its lore page. If there is
   no canon yet, this space says how a lore becomes canon and how close the
   leading contender is.
2. **Contenders.** Every other lore that still has members, highest ranked
   first, each as the same kind of block with its progress toward canon ("1
   more person to be eligible for canon"). A contender is promoted by the rank
   job when it outranks the canon; the old canon becomes a contender, never
   deleted.
3. **Standing alone.** Stories in no lore: those published in the last seven
   days that nobody has linked yet, then tales.
4. **Sidebar.** "Is this you?" first: every character in the day's stories
   that nobody holds yet, each a link straight to claiming it. Then how a
   day works in four steps, the day's counts, and who may do what here.

**Someone who was there but is not in the canon** writes their own
perspective into the same arc. If they and others claim each other, their
stories form their own lore, shown as a contender. If nobody links to it, it
stands alone and becomes a tale after seven days. Nobody has to be in the
first writer's story to belong to the day.

## The lore page

`/lores/:loreId`. One lore, in full:

- its name, if a member gave it one ("The Lot 7 tailgate"), otherwise the
  day's title; a named lore is titled by its name on the arc page too, with
  its people under it;
- its standing (canon or contender), members, upvotes and rank;
- the **cast**: every encounter, character by character, in the words of
  the story that named them;
- its perspectives in publishing order, each readable in full;
- the **thread**: a discussion among the people in it. Anyone can read it.
  Lore members and anyone with a perspective in the arc can post. Posts are
  plain text up to 1,000 characters, oldest first, and can be reported like
  anything else.

## The front page

The way in is a place (A-049): a search box, then a place page, then its
days. There is no feed of posts. Under the search the front page shows
**where people are writing**: up to six places, each with the day people
there remember most, and each card opens its place page.

## Places

**Were you in someone's story?** Every place page has a search over the
descriptions nobody has claimed in stories at the place or anywhere under
it, on any day ("gold paint", "red umbrella"). Each match links straight to
claiming it. This is how someone who does not know which day or arc to open
still finds the story they are in.

**Following.** Anyone signed in can follow a place, at any level: a store,
a mall, a town, a city or a state. A new story at the followed place, or
anywhere under it, notifies the follower once, however many of its
ancestors they follow. People who already wrote in that day's arc hear of it
as a new perspective in their arc instead. The front page lists the places
a reader follows.

A place carries the **category group** the place data gave it (Food and
drink, Nightlife, Shopping, Outdoors and parks, Arts and venues, Sports,
Travel, Education, Community, Lodging, Services). City and town pages offer
these as filter chips over the places inside them. Places with stories come
first everywhere a list of places is shown. Offices, services and care
places nobody would write about are left out at import
(`internal/place/importer/visitable.go`).
