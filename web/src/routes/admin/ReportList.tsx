import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { ReportStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Report } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";

import { actionLabel, reasonLabel, targetHref, targetKindLabel } from "./labels";
import { ResolveDialog, type Resolution } from "./ResolveDialog";

export const reportsKey = ["moderation", "listReports"] as const;
export const ADMINS_ONLY = "Only site admins can open the report queue.";
const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** The dossier of a user, where a moderator sees everything about them. */
export function dossierHref(userId: string): string {
  return `/admin/users?q=${encodeURIComponent(userId)}`;
}

function ReportItem({ report, onResolve }: { report: Report; onResolve?: () => void }) {
  const headingId = useId();
  const target = targetHref(report.targetKind, report.targetId);
  return (
    <li aria-labelledby={headingId} className="grid min-w-0 gap-2 border-b border-rule py-4">
      <h2 id={headingId} className="text-xl [overflow-wrap:anywhere]">
        {targetKindLabel(report.targetKind)}: {report.targetLabel}
      </h2>
      <p className="font-medium">{reasonLabel(report.reason)}</p>
      {report.details && <p className="[overflow-wrap:anywhere]">{report.details}</p>}
      <p className="text-sm text-ink-muted [overflow-wrap:anywhere]">
        Reported by{" "}
        {report.reporter ? (
          <Link to={dossierHref(report.reporter.userId)} className="text-accent underline underline-offset-4">
            {report.reporter.username}
          </Link>
        ) : (
          "a user"
        )}
        {report.createdAt && <> on {dateFormat.format(timestampDate(report.createdAt))}</>}
      </p>
      {report.status === ReportStatus.RESOLVED && (
        <p className="text-sm [overflow-wrap:anywhere]">
          {actionLabel(report.resolution)} by {report.resolvedBy?.username ?? "an admin"}
          {report.resolvedAt && <> on {dateFormat.format(timestampDate(report.resolvedAt))}</>}
          {report.resolutionReason && <>: {report.resolutionReason}</>}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        {target && (
          <Link to={target.href} className="text-sm text-accent underline underline-offset-4">
            {target.text}
          </Link>
        )}
        {onResolve && (
          <Button size="sm" onClick={onResolve}>
            Resolve
          </Button>
        )}
      </div>
    </li>
  );
}

/**
 * A page of reports with "Show more", resolvable while open. subjectUserId
 * narrows it to reports against one user; status UNSPECIFIED lists all.
 */
export function ReportList({
  status,
  subjectUserId = "",
  label,
  empty,
  onResolved,
}: {
  status: ReportStatus;
  subjectUserId?: string;
  label: string;
  empty: string;
  onResolved: (r: Resolution) => void;
}) {
  const queryClient = useQueryClient();
  const [resolving, setResolving] = useState<Report | null>(null);
  const list = useInfiniteQuery({
    queryKey: [...reportsKey, status, subjectUserId],
    queryFn: ({ pageParam }) => clients.moderation.listReports({ status, subjectUserId, pageToken: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  if (list.isError) {
    const e = ConnectError.from(list.error);
    return (
      <p role="alert" className="font-medium text-error">
        {e.code === Code.PermissionDenied ? ADMINS_ONLY : `Reports could not be loaded: ${e.rawMessage}`}
      </p>
    );
  }
  if (!list.data) {
    return (
      <p role="status" className="text-ink-muted">
        Loading reports…
      </p>
    );
  }
  const reports = list.data.pages.flatMap((p) => p.reports);
  if (reports.length === 0) {
    return <p className="text-ink-muted">{empty}</p>;
  }
  return (
    <>
      <ul aria-label={label}>
        {reports.map((r) => (
          <ReportItem
            key={r.id}
            report={r}
            onResolve={r.status === ReportStatus.OPEN ? () => setResolving(r) : undefined}
          />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" pending={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            Show more
          </Button>
        </div>
      )}
      {resolving && (
        <ResolveDialog
          report={resolving}
          onClose={() => setResolving(null)}
          onResolved={(r) => {
            setResolving(null);
            onResolved(r);
            void queryClient.invalidateQueries({ queryKey: reportsKey });
          }}
        />
      )}
    </>
  );
}
