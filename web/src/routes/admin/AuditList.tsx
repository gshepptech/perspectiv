import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { AuditAction } from "@/gen/perspectiv/v1/enums_pb";
import type { AuditEntry } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";

import { ReasonDialog } from "./ReasonDialog";
import { dossierHref } from "./ReportList";
import { auditActionLabel, auditTargetHref } from "./labels";

export const auditKey = ["moderation", "listAuditLog"] as const;
const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** The hides an admin can undo from the log, and what each restores. */
const restorable: Partial<Record<AuditAction, string>> = {
  [AuditAction.HIDE_PERSPECTIVE]: "perspective",
  [AuditAction.HIDE_PLACE]: "place",
  [AuditAction.HIDE_LORE_POST]: "thread post",
};

function EntryItem({ entry, onRestored }: { entry: AuditEntry; onRestored: (text: string) => void }) {
  const href = auditTargetHref(entry.targetKind, entry.targetId);
  const label = entry.targetLabel || entry.targetRef;
  const what = restorable[entry.action];
  return (
    <li className="grid min-w-0 gap-1 border-b border-rule py-3 [overflow-wrap:anywhere]">
      <p>
        {entry.actor ? (
          <Link
            to={dossierHref(entry.actor.userId)}
            className="inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4"
          >
            {entry.actor.username}
          </Link>
        ) : (
          <span className="font-medium">The command line</span>
        )}{" "}
        {auditActionLabel(entry.action)}
        {label && (
          <>
            :{" "}
            {href ? (
              <Link to={href} className="inline-flex min-h-11 items-center font-serif italic text-accent underline underline-offset-4">
                {label}
              </Link>
            ) : (
              <span className="font-serif italic">{label}</span>
            )}
          </>
        )}
      </p>
      {entry.at && <p className="text-sm text-ink-muted">{dateFormat.format(timestampDate(entry.at))}</p>}
      {entry.reason && <p className="text-sm">“{entry.reason}”</p>}
      {what && entry.targetId && (
        <ReasonDialog
          trigger="Restore…"
          title={`Restore this ${what}?`}
          description={`${label ? `“${label}” ` : ""}becomes readable again. Claims rejected while it was hidden stay rejected.`}
          confirm={`Restore ${what}`}
          act={(reason) => clients.moderation.restore({ targetKind: entry.targetKind, targetId: entry.targetId, reason })}
          onDone={() => onRestored(`The ${what} is restored.`)}
        />
      )}
    </li>
  );
}

/**
 * Audit entries newest first, with "Show more". subjectUserId narrows them to
 * actions about one user: the account, or what they wrote.
 */
export function AuditList({
  action = AuditAction.UNSPECIFIED,
  subjectUserId = "",
  label,
  empty,
}: {
  action?: AuditAction;
  subjectUserId?: string;
  label: string;
  empty: string;
}) {
  const queryClient = useQueryClient();
  const status = useRef<HTMLParagraphElement>(null);
  const [note, setNote] = useState<{ text: string; seq: number } | null>(null);
  useEffect(() => {
    if (note) status.current?.focus();
  }, [note]);
  const restored = (text: string) => {
    setNote((n) => ({ text, seq: (n?.seq ?? 0) + 1 }));
    void queryClient.invalidateQueries({ queryKey: auditKey });
  };
  const list = useInfiniteQuery({
    queryKey: [...auditKey, action, subjectUserId],
    queryFn: ({ pageParam }) => clients.moderation.listAuditLog({ action, subjectUserId, pageToken: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const entries = list.data?.pages.flatMap((p) => p.entries) ?? [];
  const more = useShowMore(entries.length, list.fetchNextPage);
  if (list.isError) {
    const e = ConnectError.from(list.error);
    return (
      <p role="alert" className="font-medium text-error">
        {e.code === Code.PermissionDenied
          ? "Only site admins can read the audit log."
          : `The audit log could not be loaded: ${e.rawMessage}`}
      </p>
    );
  }
  if (!list.data) {
    return (
      <p role="status" className="text-ink-muted">
        Loading the audit log…
      </p>
    );
  }
  if (entries.length === 0) return <p className="text-ink-muted">{empty}</p>;
  return (
    <>
      <p ref={status} tabIndex={-1} role="status" className="text-sm text-success empty:hidden">
        {note?.text}
      </p>
      <ul ref={more.list} aria-label={label}>
        {entries.map((e) => (
          <EntryItem key={e.id} entry={e} onRestored={restored} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" pending={list.isFetchingNextPage} onClick={more.showMore}>
            Show more
          </Button>
        </div>
      )}
    </>
  );
}
