import { useMutation } from "@tanstack/react-query";
import { UserCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { isOpen } from "@/components/reader/claims";
import { ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Character } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { useInvalidateAfterWrite } from "./queries";
import { useWriteGate } from "./useWriteGate";

type Props = {
  perspectiveId: string;
  authorName: string;
  /** Undefined while the perspective is still loading. */
  characters: readonly Character[] | undefined;
  /**
   * Whether the trigger is shown: the story still has someone the viewer
   * could be. The dialog itself stays mounted, so a claim that closes the
   * offer still shows its "Claim sent." confirmation.
   */
  offer: boolean;
  /**
   * Opens the dialog once on arrival, for a link that says "That was me"
   * (the arc page's "Is this you?" list). A signed-out reader is sent to
   * sign in instead.
   */
  openOnArrival?: boolean;
};

/**
 * "That was me": lists the characters another author described and lets the
 * viewer claim one. A person is one character per story, so the dialog is
 * only offered while the viewer has no claim in it; a character someone else
 * holds shows who, and one the viewer was turned down for says so.
 */
export function ClaimDialog({ perspectiveId, authorName, characters, offer, openOnArrival = false }: Props) {
  const { gate } = useWriteGate();
  const [open, setOpen] = useState(false);
  const arrived = useRef(false);
  useEffect(() => {
    if (!openOnArrival || !offer || arrived.current) return;
    arrived.current = true;
    if (gate()) setOpen(true);
  }, [openOnArrival, offer, gate]);
  const invalidate = useInvalidateAfterWrite();
  const claim = useMutation({
    mutationFn: (characterId: string) => clients.lore.claimCharacter({ characterId }),
    onSettled: () => invalidate(perspectiveId),
  });
  const sorted = characters ? [...characters].sort((a, b) => a.position - b.position) : undefined;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next && !gate()) return;
        if (next) claim.reset();
        setOpen(next);
      }}
    >
      {offer && (
        <DialogTrigger asChild>
          <Button variant="secondary" size="sm">
            <UserCheck aria-hidden size={16} />
            Claim a character
          </Button>
        </DialogTrigger>
      )}
      <DialogContent>
        <DialogTitle>Claim a character</DialogTitle>
        <DialogDescription>
          If {authorName} described you, claim that character. {authorName} decides whether to accept.
        </DialogDescription>
        {sorted === undefined ? (
          <p role="status" className="mt-4 text-sm text-ink-muted">
            Loading characters…
          </p>
        ) : sorted.length === 0 ? (
          <p className="mt-4 text-sm text-ink-muted">This perspective names no characters.</p>
        ) : (
          <ul className="mt-4 divide-y divide-rule border-y border-rule">
            {sorted.map((c) => (
              <li key={c.id} aria-label={c.label} className="flex flex-wrap items-center justify-between gap-2 py-3">
                <span className="min-w-0 font-serif break-words">{c.label}</span>
                {c.claimedBy ? (
                  <span className="text-sm text-ink-muted">Turned out to be {c.claimedBy.username}</span>
                ) : c.hasAcceptedClaim ? (
                  <span className="text-sm text-ink-muted">Already claimed</span>
                ) : c.hasPendingClaim ? (
                  <span className="text-sm text-ink-muted">Someone has claimed this</span>
                ) : c.myClaim === ClaimStatus.REJECTED || !isOpen(c) ? (
                  <span className="text-sm text-ink-muted">{authorName} said this was not you</span>
                ) : (
                  <Button size="sm" disabled={claim.isPending || claim.isSuccess} onClick={() => claim.mutate(c.id)}>
                    That was me
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {claim.isSuccess && (
          <p role="status" className="mt-4 text-sm font-medium text-success">
            Claim sent. {authorName} will accept or reject it.
          </p>
        )}
        {claim.isError && (
          <p role="alert" className="mt-4 text-sm font-medium text-error">
            {actionError(claim.error)}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}
