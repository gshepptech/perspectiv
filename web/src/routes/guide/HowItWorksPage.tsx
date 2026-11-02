import type { ReactNode } from "react";
import { Link } from "react-router";

import { CANON_MIN_MEMBERS } from "@/components/actions/rights";
import { useDocumentTitle } from "@/lib/title";
import { MAX_CHARACTERS, MAX_PARTS, MAX_WORDS_PER_PART } from "@/routes/compose/composeForm";
import { hourLabel } from "@/routes/compose/localDate";
import { ExampleArc } from "@/routes/landing/ExampleArc";

/** One section of the guide: a serif heading over its prose. */
function Part({ id, title, children }: { id: string; title: ReactNode; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="min-w-0">
      <h2 id={id} className="text-xl">
        {title}
      </h2>
      {/* One line of space between blocks (Folio, Measure and layout), set by the gap rather than prose-body's p + p margin. */}
      <div className="mt-2 grid min-w-0 gap-[1.7em] [&>p+p]:mt-0">{children}</div>
    </section>
  );
}

/** The worked example carried forward: an aside set off by a hairline, not a warning or a quote. */
function Example({ children }: { children: ReactNode }) {
  return <p className="border-l-2 border-rule pl-4">{children}</p>;
}

/**
 * /how-it-works: the whole product for a first-time visitor, each rule said
 * exactly once and followed through one worked example, the front page's
 * evening at Moonbean Coffee (Folio, How Perspectiv works). The numbers are
 * the constants the rest of the client enforces, so the page cannot drift
 * from the composer and the vote-out button.
 */
export function Component() {
  useDocumentTitle("How Perspectiv works");
  return (
    <article aria-labelledby="how-title" className="mx-auto min-w-0 max-w-measure pb-16">
      <h1 id="how-title" className="text-2xl sm:text-3xl">
        How Perspectiv works
      </h1>
      <p className="mt-3 font-serif text-prose text-ink-muted">
        Perspectiv collects the stories of people who were in the same place on the same day. This is how one evening at a
        coffee shop came to be told from every side.
      </p>

      <div className="mt-8 min-w-0">
        <ExampleArc />
      </div>

      <div className="prose-body mt-10 grid gap-10">
        <Part id="how-day" title="A place and a day">
          <p>
            Every story is pinned to a place and a day: a city, a mall or one shop, on the day it happened there, by that
            place&apos;s own calendar. You pick the day when you write, any day up to today.
          </p>
          <p>
            The first person to write about a place on a day gives the day its title, something better than the place and
            the date. Everyone who writes about that place on that day after them joins the same day.
          </p>
          <Example>
            wren was the first to write about Moonbean Coffee that Friday, so she titled the day “The night the power went
            out”.
          </Example>
        </Part>

        <Part id="how-story" title="Writing your story">
          <p>
            Your story is your side of the day, in up to {MAX_PARTS} parts of up to {MAX_WORDS_PER_PART} words each. Each
            part reads as one page of a book. You give it a genre, such as drama or comedy.
          </p>
          <p>
            A draft saves itself as you write and stays yours alone until you publish. Once it is published you can still
            edit the words, but its place, day and hour stay fixed. You write one story about a day at a place.
          </p>
          <Example>wren wrote two parts: the lights going out mid-song, and the room singing in the dark.</Example>
        </Part>

        <Part id="how-claims" title="Characters and “that was me”">
          <p>
            Instead of naming the people you saw, you describe them, like “the clerk with the green scarf”. Those
            descriptions are your story&apos;s characters, up to {MAX_CHARACTERS} of them. Never use a real name.
          </p>
          <p>
            Anyone who has published a story about the same day can open yours and say one of its characters was them:
            “That was me.” That is a request, and you accept or reject it under Requests. A character can belong to one
            person, you can be one character in each story, and someone you turn down cannot claim that character again.
            Once you accept, that character stays in your story.
          </p>
          <Example>
            otter read wren&apos;s story and recognised the guy in the yellow raincoat. He wrote his own side of that Friday
            first, then pressed “That was me”, and wren accepted.
          </Example>
        </Part>

        <Part id="how-lore" title="How stories link into a lore">
          <p>
            An accepted claim links two stories. Stories linked to each other, directly or through someone else&apos;s,
            form a lore. A third person can claim a character in either of the first two stories, a fourth in any of the
            three, and the lore grows. Each lore has its own page: who was who, every story in full, and a thread where the
            people who wrote about the day talk it over.
          </p>
          <p>
            A story nobody links to within a week of being published stands alone as a tale. It stays readable, and it
            still joins a lore if a claim on it is accepted later.
          </p>
          <Example>
            wren&apos;s and otter&apos;s stories became a lore. Two more people from that night found themselves in those
            stories, and the lore grew to four.
          </Example>
        </Part>

        <Part id="how-canon" title="How canon is chosen">
          <p>
            Canon is chosen by the people who wrote about the day, never by readers. Only they can upvote the day&apos;s
            stories: one upvote per story, never on their own, and there are no downvotes.
          </p>
          <p>
            Every minute or so the day&apos;s lores are ranked by how many people are in each and how many upvotes their
            stories have. The highest-ranked lore with {CANON_MIN_MEMBERS} or more people is the canon of the day, pinned at
            the top of its page. A smaller lore cannot be canon, however many upvotes it has. Canon can change hands: when
            another lore ranks higher, the old canon becomes a contender again, and if the canon falls below{" "}
            {CANON_MIN_MEMBERS} people the day has none until a lore qualifies. Every story stays readable either way.
          </p>
          <Example>
            The people who wrote about that Friday upvoted the stories they believed, and the lore of four became the canon
            of the night.
          </Example>
        </Part>

        <Part id="how-vote-out" title="Voting a story out">
          <p>
            Once a lore has {CANON_MIN_MEMBERS} or more people, its members can vote a story out of it when it does not fit
            what happened. Only members of that lore vote, never the story&apos;s own writer, and the writer and the members
            are told when the first vote is cast.
          </p>
          <p>
            When more than half of the other members have voted, the story leaves the lore for good: its links are undone,
            it stands alone as a tale, and it can never link again. It stays readable.
          </p>
          <Example>
            Had a fifth person joined their lore with a story the others thought did not fit, any three of the other four
            could have voted it out.
          </Example>
        </Part>

        <Part id="how-hours" title="Hours">
          <p>
            A busy day can be split by the hour. When you write you can choose one hour, in the place&apos;s local time,
            instead of the whole day. Each hour has its own stories, its own lores and its own canon, and the day&apos;s page
            lists its hours in order.
          </p>
          <p>
            Stories link only within the same day or the same hour, so choose an hour only when your story belongs to it.
            The hour is fixed once you publish.
          </p>
          <Example>
            Had wren filed her story under {hourLabel(21)}, otter would have had to write his for {hourLabel(21)} too before
            he could claim the raincoat.
          </Example>
        </Part>

        <Part id="how-readers" title="What readers can do">
          <p>Anyone can read every story, lore and thread, without an account. With one, you can also:</p>
          <ul className="grid list-disc gap-2 pl-5">
            <li>react to a story with any of six emoji, one of each;</li>
            <li>save a story to read later;</li>
            <li>follow a place or a writer, and be told when a new story arrives.</li>
          </ul>
          <p>Upvoting, claiming and posting in a lore&apos;s thread belong to the people who wrote about the day.</p>
        </Part>

        <Part id="how-privacy" title="Your privacy">
          <p>
            Other people see only your username, never your email address; the{" "}
            <Link to="/privacy" className="text-accent underline underline-offset-4">
              privacy notice
            </Link>{" "}
            says what Perspectiv keeps.
          </p>
        </Part>
      </div>
    </article>
  );
}
