import { Link } from "react-router";

import type { ViewerRights } from "@/components/actions/PerspectiveActions";
import { VoteButton } from "@/components/actions/VoteButton";
import { AuthorLink } from "@/components/reader/AuthorLink";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { cn } from "@/lib/cn";
import { useSession } from "@/lib/session";

import { genreLabel, loreStatusLabel } from "./labels";

const tones = { muted: "text-ink-muted", warning: "text-warning", success: "text-success" } as const;

/**
 * The vote rail of a row. The listing says whether the viewer voted, so no
 * story is read to draw it. Only a signed-in writer of this day gets the
 * button; everyone else sees the count, and the day page says once, above
 * its stories, who can vote (UpvoteRule).
 */
function Rail({ summary, rights }: { summary: PerspectiveSummary; rights?: ViewerRights }) {
  const session = useSession();
  const own = !!session && session.userId === summary.author?.userId;
  const canVote = !!session && !!rights && !own && rights.participant;
  const name = summary.author?.username;
  return (
    <VoteButton
      variant="rail"
      perspectiveId={summary.id}
      upvotes={summary.upvotes}
      voted={summary.voted}
      canVote={canVote}
      targetName={name ? `${name}'s perspective` : "this perspective"}
    />
  );
}

/**
 * One story on the day page, as a compact row: the vote rail, who wrote it
 * and how it stands, the opening of it, and a link to read it. Claiming,
 * reacting and reporting happen on the story's own page.
 */
export function StoryRow({
  summary,
  rights,
  level = 3,
  showStanding = true,
}: {
  summary: PerspectiveSummary;
  /** False inside a lore block, where every row is in that lore. */
  showStanding?: boolean;
  /** Adds the upvote toggle; without it the row is read-only. */
  rights?: ViewerRights;
  /** The row heading's level under the block that lists it. */
  level?: 3 | 4;
}) {
  const Heading = level === 3 ? "h3" : "h4";
  const name = summary.author?.username;
  if (summary.deleted || summary.hidden) {
    return (
      <article aria-label="Unavailable perspective" className="py-3 pl-14">
        <p className="text-sm text-ink-muted">
          {summary.deleted ? "This perspective was deleted." : "This perspective was hidden by a moderator."}
        </p>
      </article>
    );
  }
  const status = loreStatusLabel(summary.loreStatus);
  const genre = genreLabel(summary.genre);
  const href = `/perspectives/${summary.id}`;
  return (
    <article aria-label={name ? `${name}'s perspective` : "A perspective"} className="flex min-w-0 gap-2 py-3">
      <Rail summary={summary} rights={rights} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm text-ink-muted">
          <Heading className="min-w-0 font-sans text-sm font-medium break-words text-ink">
            {name ? <AuthorLink username={name} /> : "Someone"}
          </Heading>
          {genre && <span>· {genre}</span>}
          <span>
            · {summary.partCount} {summary.partCount === 1 ? "part" : "parts"}
          </span>
          {status && showStanding && <span className={cn("font-medium", tones[status.tone])}>· {status.text}</span>}
        </div>
        {summary.excerpt && (
          <p className="mt-1 line-clamp-2 max-w-measure font-serif text-prose break-words hyphens-auto text-ink">{summary.excerpt}</p>
        )}
        <Link to={href} className="mt-1 inline-flex min-h-11 min-w-11 items-center text-sm font-medium text-accent underline underline-offset-4">
          Read <span className="sr-only">{name ? `${name}'s perspective` : "this perspective"}</span>
        </Link>
      </div>
    </article>
  );
}
