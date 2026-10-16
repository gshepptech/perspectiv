import { useMutation } from "@tanstack/react-query";
import { UserMinus } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { useInvalidateAfterWrite } from "./queries";
import { useWriteGate } from "./useWriteGate";

/**
 * A remove vote by a member of the target's own lore, confirmed first. The
 * page offers it only where the server would take it (a lore of 4 or more
 * members that the viewer belongs to, never on the viewer's own perspective);
 * the server checks again.
 */
export function RemoveVoteButton({ perspectiveId, authorName }: { perspectiveId: string; authorName: string }) {
  const { gate } = useWriteGate();
  const [open, setOpen] = useState(false);
  const invalidate = useInvalidateAfterWrite();
  const remove = useMutation({
    mutationFn: () => clients.vote.voteRemove({ perspectiveId }),
    onSuccess: () => setOpen(false),
    onSettled: () => invalidate(perspectiveId),
  });
  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (next && !gate()) return;
          setOpen(next);
        }}
      >
        <DialogTrigger asChild>
          <Button variant="danger" size="sm" disabled={remove.isSuccess}>
            <UserMinus aria-hidden size={16} />
            Vote to remove
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Vote to remove {authorName}&apos;s perspective?</DialogTitle>
          <DialogDescription>
            When more than half of the lore&apos;s other members vote to remove it, it leaves the lore and stays readable as a
            tale. Your vote cannot be taken back.
          </DialogDescription>
          {remove.isError && (
            <p role="alert" className="mt-4 text-sm font-medium text-error">
              {actionError(remove.error)}
            </p>
          )}
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
              Vote to remove
            </Button>
          </div>
        </DialogContent>
      </Dialog>
      {remove.data && (
        <p role="status" className="w-full text-sm text-ink-muted">
          {remove.data.removed
            ? "This perspective is now removed from the lore."
            : `${remove.data.removeVotes} of ${remove.data.eligibleVoters} members have voted to remove it.`}
        </p>
      )}
    </>
  );
}
