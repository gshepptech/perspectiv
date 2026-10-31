import { useMutation } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useRef, useState } from "react";

import { actionError } from "@/components/actions/errors";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { clients } from "@/lib/connect";

import { useInvalidateThread } from "./threadQueries";

/** Deletes the viewer's own post after asking first. `onGone` runs once it is deleted. */
export function DeletePostDialog({ postId, hasReplies, onGone }: { postId: string; hasReplies: boolean; onGone: () => void }) {
  const [open, setOpen] = useState(false);
  const invalidate = useInvalidateThread();
  // Once the post is gone so is the trigger the dialog would hand focus
  // back to, so focus goes where `onGone` says instead.
  const deleted = useRef(false);
  const remove = useMutation({
    mutationFn: () => clients.lore.deleteLorePost({ postId }),
    onSuccess: async () => {
      deleted.current = true;
      setOpen(false);
      await invalidate();
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) remove.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="ghost" size="sm" className="min-h-11 text-ink-muted">
          <Trash2 aria-hidden size={16} />
          Delete
        </Button>
      </DialogTrigger>
      <DialogContent
        onCloseAutoFocus={(e) => {
          if (!deleted.current) return;
          e.preventDefault();
          onGone();
        }}
      >
        <DialogTitle>Delete your post?</DialogTitle>
        <DialogDescription>
          {hasReplies
            ? "Its replies stay, under a post marked [deleted]. This cannot be undone."
            : "It leaves the thread. This cannot be undone."}
        </DialogDescription>
        {remove.isError && (
          <p role="alert" className="mt-3 text-sm text-error">
            {actionError(remove.error)}
          </p>
        )}
        <div className="mt-6 flex flex-wrap gap-2">
          <Button variant="danger" pending={remove.isPending} onClick={() => remove.mutate()}>
            Delete
          </Button>
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  );
}
