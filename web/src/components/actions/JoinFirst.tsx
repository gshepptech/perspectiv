import { PenLine } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { arcSpan, type ArcSpan } from "@/components/reader/format";
import { useSession } from "@/lib/session";
import { composeHref, type Where } from "@/routes/compose/links";

/** The action a reader pressed before they were able to take it. */
export type Blocked = "claim" | "upvote" | "react";

const why: Record<Blocked, (span: ArcSpan) => string> = {
  claim: (span) => `To say this was you, write your side of this ${span} first. Your story and theirs link once they accept.`,
  // Most readers were not there, so the rule is said first and writing is
  // offered only to someone who was.
  upvote: (span) => `Only people who wrote about this ${span} can upvote its stories, so the people who were there choose its canon.`,
  // Any member may react, so only a signed-out reader is ever told this.
  react: () => "To react to a story, sign in or create an account.",
};

const linkClass = "inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4";

/**
 * Who may upvote a day's stories, and the way in for someone who was
 * there: the composer for that place and day (which asks a signed-out
 * reader to sign in first), and, signed out, sign-in for someone who
 * already wrote theirs. `back` is where both return to. The story page
 * says it under Upvote; the day page says it once above its stories.
 */
export function UpvoteRule({ where, back }: { where: Where; back: string }) {
  const session = useSession();
  return (
    <>
      <p>{why.upvote(arcSpan(where.hour))}</p>
      <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
        <span>
          <span>Were you there?</span>{" "}
          <Link to={composeHref(where, back)} className={linkClass}>
            Write your side
          </Link>
        </span>
        {!session && (
          <span>
            <span>Already wrote yours?</span>{" "}
            <Link to={`/login?next=${encodeURIComponent(back)}`} className={linkClass}>
              Sign in
            </Link>
          </span>
        )}
      </p>
    </>
  );
}

/**
 * Why a reader cannot claim or upvote yet, said in place of the action, and
 * the way to become able to: the composer for this story's place and day,
 * or, signed out, sign-in and sign-up. Each leads back to this story, and
 * for a claim the story then opens with the claim dialog up, on the
 * character the reader came for when `characterId` names one.
 */
export function JoinFirst({
  id,
  reason,
  p,
  focus = false,
  characterId,
}: {
  id: string;
  reason: Blocked;
  p: Perspective;
  focus?: boolean;
  characterId?: string;
}) {
  const session = useSession();
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focus) panel.current?.focus();
  }, [focus]);
  const story = `/perspectives/${encodeURIComponent(p.id)}`;
  const back = reason === "claim" ? `${story}?claim=${encodeURIComponent(characterId || "1")}` : story;
  const next = encodeURIComponent(back);
  const where: Where = { placeId: p.place?.id ?? "", localDate: p.localDate, hour: p.hour };
  return (
    <div id={id} ref={panel} tabIndex={-1} className="w-full rounded-md border border-rule bg-surface p-4 text-sm text-ink">
      {reason === "upvote" ? (
        <UpvoteRule where={where} back={back} />
      ) : (
        <>
          <p>{why[reason](arcSpan(p.hour))}</p>
          <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
            {session ? (
              <Button asChild size="sm" className="min-h-11">
                <Link to={composeHref(where, back)}>
                  <PenLine aria-hidden size={16} />
                  Write your side
                </Link>
              </Button>
            ) : (
              <>
                <Button asChild size="sm" className="min-h-11">
                  <Link to={`/login?next=${next}`}>Sign in</Link>
                </Button>
                <Link to={`/signup?next=${next}`} className={linkClass}>
                  Create an account
                </Link>
              </>
            )}
          </p>
        </>
      )}
    </div>
  );
}
