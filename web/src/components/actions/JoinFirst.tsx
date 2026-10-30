import { PenLine } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { useSession } from "@/lib/session";
import { composeHref } from "@/routes/compose/links";

/** The action a reader pressed before they were able to take it. */
export type Blocked = "claim" | "upvote";

const why: Record<Blocked, string> = {
  claim: "To say this was you, write your side of this day first. Your story and theirs link once they accept.",
  upvote: "Only people who wrote about this day can upvote its stories. Write your side first.",
};

const linkClass = "inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4";

/**
 * Why a reader cannot claim or upvote yet, said in place of the action, and
 * the way to become able to: the composer for this story's place and day,
 * or, signed out, sign-in and sign-up. Each leads back to this story, and
 * for a claim the story then opens with the claim dialog up.
 */
export function JoinFirst({ id, reason, p, focus = false }: { id: string; reason: Blocked; p: Perspective; focus?: boolean }) {
  const session = useSession();
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focus) panel.current?.focus();
  }, [focus]);
  const story = `/perspectives/${encodeURIComponent(p.id)}`;
  const back = reason === "claim" ? `${story}?claim=1` : story;
  const next = encodeURIComponent(back);
  return (
    <div id={id} ref={panel} tabIndex={-1} className="w-full rounded-md border border-rule bg-surface p-4 text-sm text-ink">
      <p>{why[reason]}</p>
      <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
        {session ? (
          <Button asChild size="sm" className="min-h-11">
            <Link to={composeHref({ placeId: p.place?.id ?? "", localDate: p.localDate, hour: p.hour }, back)}>
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
    </div>
  );
}
