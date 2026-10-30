import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bookmark } from "lucide-react";

import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { perspectiveKey, savedListKey } from "./queries";
import { useWriteGate } from "./useWriteGate";

/**
 * The Save toggle: keeps a story on the reader's Saved page to read later.
 * A signed-out reader who presses it is sent to sign in and back. The
 * server's answer goes into the story's cache at once, and the Saved page's
 * list is refetched when next shown.
 */
export function SaveButton({ perspectiveId, saved, targetName }: { perspectiveId: string; saved: boolean; targetName: string }) {
  const { gate } = useWriteGate();
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (on: boolean) => clients.perspective.savePerspective({ perspectiveId, on }),
    onSuccess: (res) => {
      client.setQueryData<Perspective | null>(perspectiveKey(perspectiveId), (old) => (old ? { ...old, saved: res.saved } : old));
    },
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: perspectiveKey(perspectiveId) }),
        client.invalidateQueries({ queryKey: savedListKey }),
      ]),
  });
  const label = saved ? "Saved" : "Save";
  return (
    <>
      <Button
        variant={saved ? "secondary" : "ghost"}
        size="sm"
        className="min-h-11"
        aria-pressed={saved}
        aria-label={`${label} ${targetName}`}
        pending={save.isPending}
        onClick={() => {
          if (gate()) save.mutate(!saved);
        }}
      >
        <Bookmark aria-hidden size={16} fill={saved ? "currentColor" : "none"} />
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
