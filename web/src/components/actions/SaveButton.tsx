import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bookmark } from "lucide-react";

import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { perspectiveKey, savedListKey } from "./queries";
import { useWriteGate } from "./useWriteGate";

type Props = {
  perspectiveId: string;
  saved: boolean;
  targetName: string;
  /** The server's answer, for a list that keeps its own copy of `saved`. */
  onSaved?: (saved: boolean) => void;
  /** Off, the toggle reads "Undo" and saves the story again (the Saved page's removed rows). */
  undo?: boolean;
};

/**
 * The Save toggle: keeps a story on the reader's Saved page to read later.
 * A signed-out reader who presses it is sent to sign in and back. The
 * server's answer goes into the story's cache at once. The Saved page's
 * list is marked stale but not refetched, so a row unsaved there stays,
 * with Undo, until the reader leaves; it is read again when next shown.
 */
export function SaveButton({ perspectiveId, saved, targetName, onSaved, undo = false }: Props) {
  const { gate } = useWriteGate();
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (on: boolean) => clients.perspective.savePerspective({ perspectiveId, on }),
    onSuccess: (res) => {
      client.setQueryData<Perspective | null>(perspectiveKey(perspectiveId), (old) => (old ? { ...old, saved: res.saved } : old));
      onSaved?.(res.saved);
    },
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: perspectiveKey(perspectiveId) }),
        client.invalidateQueries({ queryKey: savedListKey, refetchType: "none" }),
      ]),
  });
  const undoing = undo && !saved;
  const label = undoing ? "Undo" : saved ? "Saved" : "Save";
  return (
    <>
      <Button
        variant={saved ? "secondary" : "ghost"}
        size="sm"
        // Flush only while ghost: the secondary border must not hang outside the column.
        flush={!saved}
        className="min-h-11"
        // Undo is an action, not the toggle's state, so it is not pressed or unpressed.
        aria-pressed={undoing ? undefined : saved}
        aria-label={undoing ? `Undo: save ${targetName} again` : `${label} ${targetName}`}
        pending={save.isPending}
        onClick={() => {
          if (gate()) save.mutate(!saved);
        }}
      >
        {!undoing && <Bookmark aria-hidden size={16} fill={saved ? "currentColor" : "none"} />}
        {label}
      </Button>
      {save.isError && (
        <p role="alert" className="w-full text-sm font-medium text-error">
          {actionError(save.error)}
        </p>
      )}
    </>
  );
}
