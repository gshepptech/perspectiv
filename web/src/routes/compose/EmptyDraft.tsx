import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";

/**
 * Offered when every part of a saved draft has been cleared (Folio,
 * Composer: clearing a draft). Autosave waits for text and the server
 * refuses an empty draft, so the draft's old text is still stored and
 * would come back the next time it resumes; this asks whether to delete it.
 * Nothing is deleted without the press.
 */
export function EmptyDraft({
  perspectiveId,
  settle,
  onKeepWriting,
  onDeleted,
}: {
  perspectiveId: string;
  /** Resolves once no save is pending or in flight, so none lands after the delete. */
  settle: () => Promise<void>;
  onKeepWriting: () => void;
  onDeleted: () => void | Promise<void>;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: async () => {
      await settle();
      await clients.perspective.delete({ perspectiveId });
    },
    onSuccess: async () => {
      // The front page's "Your stories" and the composer's own draft lookup.
      await queryClient.invalidateQueries({ queryKey: ["perspective", "listMine"] });
      await onDeleted();
    },
  });
  return (
    <div className="grid min-w-0 gap-3 rounded-md border border-rule bg-surface p-3 text-sm text-ink">
      <p>This draft is empty. Delete it?</p>
      <div className="flex flex-wrap gap-2">
        <Button variant="danger" pending={remove.isPending} onClick={() => remove.mutate()}>
          Delete
        </Button>
        <Button variant="secondary" onClick={onKeepWriting}>
          Keep writing
        </Button>
      </div>
      {remove.isError && (
        <p role="alert" className="font-medium text-error">
          Not deleted. Try again in a moment.
        </p>
      )}
    </div>
  );
}
