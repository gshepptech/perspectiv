import { useEffect, useRef } from "react";
import { useSearchParams } from "react-router";

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import { AuditAction, Role } from "@/gen/perspectiv/v1/enums_pb";
import { useSession } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";

import { AuditList } from "./AuditList";
import { auditActionLabel, auditActions } from "./labels";

const ALL = "all";

/** The action filter's value in the address, as the enum's name. */
function actionFrom(value: string | null): AuditAction {
  const found = auditActions.find((a) => AuditAction[a] === value);
  return found ?? AuditAction.UNSPECIFIED;
}

/**
 * /admin/audit: every admin action, newest first, filterable by action. The
 * filter lives in the address so a filtered view is linkable.
 */
export function Component() {
  useDocumentTitle("Audit log");
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const action = actionFrom(params.get("action"));
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <section aria-labelledby="audit-title" className="mx-auto grid w-full max-w-shell min-w-0 gap-4">
      <h1 id="audit-title" ref={heading} tabIndex={-1} className="text-2xl focus:outline-none sm:text-3xl">
        Audit log
      </h1>
      {session?.role !== Role.ADMIN ? (
        <p className="text-ink-muted">Only site admins can read the audit log.</p>
      ) : (
        <>
          <Select
            value={action === AuditAction.UNSPECIFIED ? ALL : AuditAction[action]}
            onValueChange={(v) => setParams(v === ALL ? {} : { action: v }, { replace: true })}
          >
            <SelectTrigger aria-label="Action" className="w-auto min-w-56 justify-self-start">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>All actions</SelectItem>
              {auditActions.map((a) => (
                <SelectItem key={a} value={AuditAction[a]}>
                  {auditActionLabel(a).replace(/^./, (c) => c.toUpperCase())}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <AuditList
            action={action}
            label="Audit entries"
            empty={action === AuditAction.UNSPECIFIED ? "Nothing recorded yet." : "No entries for this action."}
          />
        </>
      )}
    </section>
  );
}
