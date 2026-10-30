import { Link } from "react-router";

import { ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Character } from "@/gen/perspectiv/v1/perspective_pb";

import { AuthorLink } from "./AuthorLink";

/**
 * The people a story describes, each with who it turned out to be: an
 * accepted claim tags the character with the claimant's name. The tag opens
 * the claimant's own story of the day while it is readable, else their
 * profile. The viewer's own pending claim shows only to them.
 */
export function Cast({ characters, className = "" }: { characters: readonly Character[]; className?: string }) {
  if (characters.length === 0) return null;
  const sorted = [...characters].sort((a, b) => a.position - b.position);
  return (
    <ul aria-label="Cast" className={`flex flex-wrap gap-2 text-sm ${className}`}>
      {sorted.map((c) => {
        const story = c.claimedBy ? c.claimedByStoryId : "";
        return (
          <li key={c.id} className="inline-flex min-w-0 flex-wrap items-center gap-1 rounded-full border border-rule bg-paper px-3 py-1">
            <span className="min-w-0 font-serif wrap-anywhere text-ink">{c.label}</span>
            {c.claimedBy ? (
              <>
                <span aria-hidden className="text-ink-muted">
                  ·
                </span>
                {story ? (
                  <Link to={`/perspectives/${encodeURIComponent(story)}`} className="wrap-anywhere text-accent underline underline-offset-4">
                    {c.claimedBy.username}
                    <span className="sr-only">’s story</span>
                  </Link>
                ) : (
                  <AuthorLink username={c.claimedBy.username} />
                )}
              </>
            ) : c.myClaim === ClaimStatus.PENDING ? (
              <span className="text-warning">· your claim is pending</span>
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}
