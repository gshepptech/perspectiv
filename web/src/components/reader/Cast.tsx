import { UserCheck, UserRound } from "lucide-react";
import { Link } from "react-router";

import { ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Character } from "@/gen/perspectiv/v1/perspective_pb";
import { cn } from "@/lib/cn";

import { AuthorLink } from "./AuthorLink";

/**
 * The people a story describes, each with who it turned out to be: an
 * accepted claim tags the character with the claimant's name. The tag opens
 * the claimant's own story of the day while it is readable, else their
 * profile. The viewer's own pending claim shows only to them. A plain list
 * of tags, one to a line (Folio, Flairs and tags); a username wraps only
 * where it must, never mid-name when the line has room for it.
 */
export function Cast({ characters, className }: { characters: readonly Character[]; className?: string }) {
  if (characters.length === 0) return null;
  const sorted = [...characters].sort((a, b) => a.position - b.position);
  return (
    <ul aria-label="Cast" className={cn("grid gap-1 text-sm", className)}>
      {sorted.map((c) => {
        const story = c.claimedBy ? c.claimedByStoryId : "";
        const Icon = c.claimedBy ? UserCheck : UserRound;
        return (
          <li key={c.id} className="flex min-w-0 items-start gap-1.5">
            <Icon aria-hidden size={16} className="mt-0.5 shrink-0 text-ink-muted" />
            <span className="min-w-0 wrap-break-word">
              <span className="font-serif text-ink">{c.label}</span>
              {c.claimedBy ? (
                <>
                  <span aria-hidden className="text-ink-muted">
                    {" · "}
                  </span>
                  {story ? (
                    <Link to={`/perspectives/${encodeURIComponent(story)}`} className="wrap-break-word text-accent underline underline-offset-4">
                      {c.claimedBy.username}
                      <span className="sr-only">’s story</span>
                    </Link>
                  ) : (
                    <AuthorLink username={c.claimedBy.username} className="wrap-break-word" />
                  )}
                </>
              ) : c.myClaim === ClaimStatus.PENDING ? (
                <span className="text-warning"> · your claim is pending</span>
              ) : null}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
