import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { AuditAction } from "@/gen/perspectiv/v1/enums_pb";
import type { AuditEntry } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";

import { dossierHref } from "./ReportList";
import { auditActionLabel, auditTargetHref } from "./labels";

export const auditKey = ["moderation", "listAuditLog"] as const;
const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function EntryItem({ entry }: { entry: AuditEntry }) {
  const href = auditTargetHref(entry.targetKind, entry.targetId);
  const label = entry.targetLabel || entry.targetRef;
  return (
    <li className="grid min-w-0 gap-1 border-b border-rule py-3 [overflow-wrap:anywhere]">
      <p>
        {entry.actor ? (
          <Link to={dossierHref(entry.actor.userId)} className="font-medium text-accent underline underline-offset-4">
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
              <Link to={href} className="font-serif italic text-accent underline underline-offset-4">
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
  const list = useInfiniteQuery({
    queryKey: [...auditKey, action, subjectUserId],
    queryFn: ({ pageParam }) => clients.moderation.listAuditLog({ action, subjectUserId, pageToken: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
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
  const entries = list.data.pages.flatMap((p) => p.entries);
  if (entries.length === 0) return <p className="text-ink-muted">{empty}</p>;
  return (
    <>
      <ul aria-label={label}>
        {entries.map((e) => (
          <EntryItem key={e.id} entry={e} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" pending={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            Show more
          </Button>
        </div>
      )}
    </>
  );
}
