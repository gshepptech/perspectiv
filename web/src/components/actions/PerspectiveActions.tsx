import { useQuery } from "@tanstack/react-query";

import { Cast } from "@/components/reader/Cast";
import { hasOpenCharacter, myCharacter } from "@/components/reader/claims";
import { ClaimStatus, LoreStatus, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { useSession } from "@/lib/session";

import { ClaimDialog } from "./ClaimDialog";
import { perspectiveQuery } from "./queries";
import { ReactBar } from "./ReactBar";
import { RemoveVoteButton } from "./RemoveVoteButton";
import { ReportDialog } from "./ReportDialog";
import { VoteButton } from "./VoteButton";

/** What the page knows about the viewer's place in this perspective's arc. */
export type ViewerRights = {
  /** The viewer has a published perspective in the same arc. */
  participant: boolean;
  /** The viewer is a member of this perspective's lore, and that lore has 4 or more members. */
  loreVoter: boolean;
};

/**
 * The actions under one perspective: up-vote, reacts, claim, remove vote and
 * report. Each is offered where the server would accept it, and a signed-out
 * reader sees the ones open to any account, which send them to sign in. The
 * server enforces every rule again; hiding a button is usability only.
 *
 * The listing summary has no vote, react or character state, so the full
 * perspective is read once per card (and shared with the reader page's cache).
 * A deleted or hidden perspective has no actions and is never fetched.
 */
export function PerspectiveActions({ summary, rights }: { summary: PerspectiveSummary; rights: ViewerRights }) {
  const session = useSession();
  const gone = summary.deleted || summary.hidden;
  const full = useQuery({ ...perspectiveQuery(summary.id), enabled: !gone });
  if (gone) return null;

  const p = full.data;
  const authorName = summary.author?.username ?? "the author";
  const own = !!session && session.userId === summary.author?.userId;
  const signedOut = !session;
  const canVote = !own && (signedOut || rights.participant);
  const mine = p ? myCharacter(p.characters) : undefined;
  // Offered only when the story has someone the viewer could still be.
  const canClaim = !own && summary.loreStatus !== LoreStatus.REMOVED && (signedOut || rights.participant) && !!p;
  const canVoteOut = !!session && !own && summary.loreStatus === LoreStatus.IN_LORE && rights.loreVoter;

  return (
    <div className="mt-3 flex flex-col gap-3 border-t border-rule pt-3">
      {p && <Cast characters={p.characters} />}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <VoteButton perspectiveId={summary.id} upvotes={p?.upvotes ?? summary.upvotes} voted={p?.voted ?? false} canVote={canVote} />
        {p && <ReactBar perspectiveId={summary.id} counts={p.reacts} />}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {canClaim && <ClaimDialog perspectiveId={summary.id} authorName={authorName} characters={p.characters} offer={hasOpenCharacter(p.characters)} />}
        {mine && (
          <p className="text-sm text-ink">
            You are <span className="font-serif">{mine.label}</span>
            {mine.myClaim === ClaimStatus.PENDING ? (
              <span className="text-warning"> · waiting for {authorName} to accept</span>
            ) : (
              <span className="text-success"> · accepted</span>
            )}
          </p>
        )}
        {canVoteOut && <RemoveVoteButton perspectiveId={summary.id} authorName={authorName} />}
        {!own && (
          <ReportDialog
            targetKind={ReportTargetKind.PERSPECTIVE}
            targetId={summary.id}
            targetLabel={`${authorName}'s perspective`}
          />
        )}
      </div>
    </div>
  );
}
