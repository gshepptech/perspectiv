import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/Button";
import type { SessionInfo } from "@/gen/perspectiv/v1/auth_pb";
import { clients } from "@/lib/connect";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

const sessionsKey = ["auth", "listSessions"] as const;
const day = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "5 minutes ago", "yesterday", "3 days ago": when a device was last used. */
export function lastUsed(at: Date, now: Date): string {
  const minutes = Math.round((at.getTime() - now.getTime()) / 60_000);
  if (minutes > -2) return "just now";
  if (minutes > -60) return relative.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (hours > -24) return relative.format(hours, "hour");
  return relative.format(Math.round(hours / 24), "day");
}

function DeviceRow({ s, onSignedOut }: { s: SessionInfo; onSignedOut: (device: string) => void }) {
  const queryClient = useQueryClient();
  const name = s.device || "A browser we did not recognise";
  const signOut = useMutation({
    mutationFn: () => clients.auth.revokeSession({ sessionId: s.sessionId }),
    onSuccess: async () => {
      onSignedOut(name);
      await queryClient.invalidateQueries({ queryKey: sessionsKey });
    },
  });
  return (
    <li className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-rule py-3">
      <div className="min-w-0">
        <p className="font-medium [overflow-wrap:anywhere]">
          {name}
          {s.current && <span className="ml-2 text-sm font-normal text-success">This device</span>}
        </p>
        <p className="text-sm text-ink-muted">
          Signed in {s.signedInAt ? day.format(timestampDate(s.signedInAt)) : "earlier"}
          {s.lastUsedAt && !s.current && <>, last used {lastUsed(timestampDate(s.lastUsedAt), new Date())}</>}
        </p>
        <FormAlert message={signOut.error ? errorMessage(signOut.error) : null} />
      </div>
      {!s.current && (
        <Button variant="secondary" size="sm" className="min-h-11" pending={signOut.isPending} onClick={() => signOut.mutate()}>
          Sign out <span className="sr-only">{name}</span>
        </Button>
      )}
    </li>
  );
}

/**
 * The account's signed-in devices, this one first among equals, each other
 * one with its own "Sign out". Only a short browser-and-system label is kept
 * for a device, never the full browser string or an address.
 */
export function DeviceList() {
  const sessions = useQuery({ queryKey: sessionsKey, queryFn: () => clients.auth.listSessions({}) });
  const status = useRef<HTMLParagraphElement>(null);
  const [note, setNote] = useState<{ text: string; seq: number } | null>(null);
  // The row and its button go away, so focus moves to what happened.
  useEffect(() => {
    if (note) status.current?.focus();
  }, [note]);
  if (sessions.isError) return <FormAlert message={errorMessage(sessions.error)} />;
  if (!sessions.data) {
    return (
      <p role="status" className="text-ink-muted">
        Loading your devices…
      </p>
    );
  }
  return (
    <>
      <p ref={status} tabIndex={-1} role="status" aria-label="Devices status" className="text-sm text-success">
        {note?.text}
      </p>
      <ul aria-label="Signed-in devices">
        {sessions.data.sessions.map((s) => (
          <DeviceRow
            key={s.sessionId}
            s={s}
            onSignedOut={(device) => setNote((n) => ({ text: `${device} is signed out.`, seq: (n?.seq ?? 0) + 1 }))}
          />
        ))}
      </ul>
    </>
  );
}
