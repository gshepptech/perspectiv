import { useQuery } from "@tanstack/react-query";
import { UserCheck } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Cast } from "@/components/reader/Cast";
import { hasOpenCharacter, myCharacter } from "@/components/reader/claims";
import { ClaimStatus, LoreStatus, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { useSession } from "@/lib/session";

import { Button } from "@/components/ui/Button";

import { ClaimDialog } from "./ClaimDialog";
import { JoinFirst, type Blocked } from "./JoinFirst";
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
  /** True while where the viewer stands is still being read; nothing is offered or refused until it is known. */
  pending?: boolean;
};

/**
 * The actions under one perspective: up-vote, reacts, claim, remove vote and
 * report. Each is offered where the server would accept it. Upvote and claim
 * need a perspective in the same arc, so a reader without one (signed out
 * included) who presses either is told so in place, with the way to write
 * their side or sign in. A signed-out reader who reacts is told the same way
 * that reacting needs an account; report sends them to sign in. The server
 * enforces every rule again; hiding a button is usability only.
 *
 * The listing summary has no vote, react or character state, so the full
 * perspective is read once per card (and shared with the reader page's cache).
 * A deleted or hidden perspective has no actions and is never fetched.
 */
export function PerspectiveActions({
  summary,
  rights,
  withCast = true,
  claimOnArrival = false,
  claimCharacterId,
  justPublished = false,
}: {
  summary: PerspectiveSummary;
  rights: ViewerRights;
  /** False when the page already shows the story's cast. */
  withCast?: boolean;
  /** Opens the claim dialog on arrival (a "That was me" link). */
  claimOnArrival?: boolean;
  /** The character that "That was me" link quoted, picked out in the dialog. */
  claimCharacterId?: string;
  /** The reader was sent back here right after publishing their own story. */
  justPublished?: boolean;
}) {
  const session = useSession();
  const gone = summary.deleted || summary.hidden;
  const full = useQuery({ ...perspectiveQuery(summary.id), enabled: !gone });
  const mineLine = useRef<HTMLParagraphElement>(null);
  const whyId = useId();
  const [why, setWhy] = useState<Blocked | null>(null);
  const [focusWhy, setFocusWhy] = useState(false);

  const p = full.data;
  const authorName = summary.author?.username ?? "the author";
  const own = !!session && session.userId === summary.author?.userId;
  const participant = !!session && rights.participant;
  // A reader who may not upvote or claim yet is told why when they try.
  const blocked = !own && !participant && !rights.pending;
  const canVote = !own && participant;
  const mine = p ? myCharacter(p.characters) : undefined;
  const claimable = summary.loreStatus !== LoreStatus.REMOVED && !!p;
  const canClaim = !own && participant && claimable;
  // Offered only when the story has someone the reader could still be.
  const claimBlocked = blocked && claimable && hasOpenCharacter(p.characters);
  const canVoteOut = !!session && !own && summary.loreStatus === LoreStatus.IN_LORE && rights.loreVoter;

  // "That was me" from a reader who cannot claim yet: the reason opens at
  // once and takes focus, where the claim dialog would have opened.
  const arrived = useRef(false);
  useEffect(() => {
    if (!claimOnArrival || !claimBlocked || arrived.current) return;
    arrived.current = true;
    setWhy("claim");
    setFocusWhy(true);
  }, [claimOnArrival, claimBlocked]);
  if (gone) return null;
  const toggle = (reason: Blocked) => {
    setFocusWhy(false);
    setWhy(why === reason ? null : reason);
  };

  return (
    <div className="mt-3 flex flex-col gap-3 border-t border-rule pt-3">
      {p && withCast && <Cast characters={p.characters} />}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <VoteButton
          perspectiveId={summary.id}
          upvotes={p?.upvotes ?? summary.upvotes}
          voted={p?.voted ?? false}
          canVote={canVote}
          targetName={`${authorName}'s perspective`}
          blocked={blocked && p ? { expanded: why === "upvote", controls: whyId, onPress: () => toggle("upvote") } : undefined}
        />
        {p && (
          <ReactBar
            perspectiveId={summary.id}
            counts={p.reacts}
            blocked={session ? undefined : { expanded: why === "react", controls: whyId, onPress: () => toggle("react") }}
          />
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {canClaim && (
          <ClaimDialog
            perspectiveId={summary.id}
            authorName={authorName}
            characters={p.characters}
            offer={hasOpenCharacter(p.characters)}
            openOnArrival={claimOnArrival}
            highlight={claimCharacterId}
            returnFocusTo={mineLine}
            justPublished={justPublished}
          />
        )}
        {claimBlocked && (
          <Button
            variant="secondary"
            size="sm"
            className="min-h-11"
            aria-expanded={why === "claim"}
            aria-controls={why === "claim" ? whyId : undefined}
            onClick={() => toggle("claim")}
          >
            <UserCheck aria-hidden size={16} />
            Claim a character
          </Button>
        )}
        {mine && (
          <p ref={mineLine} tabIndex={-1} className="text-sm text-ink">
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
      {why && p && <JoinFirst id={whyId} reason={why} p={p} focus={focusWhy} characterId={claimCharacterId} />}
    </div>
  );
}
