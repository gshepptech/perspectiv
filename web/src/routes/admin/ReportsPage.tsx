import { useState } from "react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { ReportStatus, Role } from "@/gen/perspectiv/v1/enums_pb";
import { useSession } from "@/lib/session";

import { actionLabel } from "./labels";
import { ADMINS_ONLY, ReportList } from "./ReportList";
import type { Resolution } from "./ResolveDialog";

/**
 * /admin/reports: the moderation queue. The server refuses ListReports and
 * Resolve to anyone without the ADMIN role; this page does not even ask for
 * them when the signed-in user is not an admin.
 */
export function Component() {
  const session = useSession();
  const [tab, setTab] = useState("open");
  const [last, setLast] = useState<Resolution | null>(null);
  return (
    <section aria-labelledby="reports-title" className="mx-auto grid w-full max-w-shell min-w-0 gap-4">
      <h1 id="reports-title" className="text-2xl sm:text-3xl">
        Report queue
      </h1>
      {session?.role !== Role.ADMIN ? (
        <p className="text-ink-muted">{ADMINS_ONLY}</p>
      ) : (
        <>
          <p role="status" aria-label="Queue status" className="text-sm text-success">
            {last &&
              (last.suspended
                ? `${last.suspended} is suspended. The report on their perspective stays open.`
                : `Report resolved: ${actionLabel(last.action).toLowerCase()}.`)}
          </p>
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList aria-label="Report status">
              <TabsTrigger value="open">Open</TabsTrigger>
              <TabsTrigger value="resolved">Resolved</TabsTrigger>
            </TabsList>
            <TabsContent value="open">
              <ReportList status={ReportStatus.OPEN} label="Open reports" empty="No open reports." onResolved={setLast} />
            </TabsContent>
            <TabsContent value="resolved">
              <ReportList
                status={ReportStatus.RESOLVED}
                label="Resolved reports"
                empty="No resolved reports yet."
                onResolved={setLast}
              />
            </TabsContent>
          </Tabs>
        </>
      )}
    </section>
  );
}
