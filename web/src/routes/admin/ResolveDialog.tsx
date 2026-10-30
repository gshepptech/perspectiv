import { ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useRef } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/Dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Textarea } from "@/components/ui/Textarea";
import { ModerationAction, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Report } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";

import { choicesFor, SUSPEND_AUTHOR, targetKindLabel } from "./labels";

const schema = z.object({
  action: z.string(),
  reason: z.string().refine((r) => r.trim() !== "", "Say why. The reason is written to the audit log."),
});
type Values = z.infer<typeof schema>;

/** What a resolution did, for the queue's status line. */
export type Resolution = { action: ModerationAction; suspended?: string };

const NO_AUTHOR = "This perspective no longer shows its author, so they cannot be suspended from here.";

/**
 * Suspends the author of a reported perspective: the server applies
 * SUSPEND_USER only to a report on a user, so the admin files one on the
 * author (or reuses their open one) and resolves it.
 */
async function suspendAuthor(report: Report, reason: string): Promise<Resolution> {
  const { perspective } = await clients.perspective.get({ perspectiveId: report.targetId });
  const author = perspective?.author;
  if (!author?.userId) throw new Error(NO_AUTHOR);
  const filed = await clients.moderation.report({
    targetKind: ReportTargetKind.USER,
    targetId: author.userId,
    reason: report.reason,
    details: `Filed from the report on perspective ${report.targetId}: ${reason}`,
  });
  await clients.moderation.resolve({ reportId: filed.reportId, action: ModerationAction.SUSPEND_USER, reason });
  return { action: ModerationAction.SUSPEND_USER, suspended: author.username };
}

export function ResolveDialog({
  report,
  onClose,
  onResolved,
}: {
  report: Report;
  onClose: () => void;
  onResolved: (r: Resolution) => void;
}) {
  const choices = choicesFor(report.targetKind);
  // Once resolved, the page moves focus to its status line: the Resolve
  // button that opened this dialog usually goes with the open report.
  const resolved = useRef(false);
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { action: choices[0]?.value ?? "", reason: "" },
  });
  const resolve = useMutation({
    mutationFn: async (v: Values): Promise<Resolution> => {
      const reason = v.reason.trim();
      if (v.action === SUSPEND_AUTHOR) return suspendAuthor(report, reason);
      const action = Number(v.action) as ModerationAction;
      await clients.moderation.resolve({ reportId: report.id, action, reason });
      return { action };
    },
    onSuccess: (r) => {
      resolved.current = true;
      onResolved(r);
    },
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        onCloseAutoFocus={(e) => {
          if (resolved.current) e.preventDefault();
        }}
      >
        <DialogTitle>Resolve report</DialogTitle>
        <DialogDescription className="[overflow-wrap:anywhere]">
          {targetKindLabel(report.targetKind)}: {report.targetLabel}
        </DialogDescription>
        <Form {...form}>
          <form noValidate onSubmit={form.handleSubmit((v) => resolve.mutate(v))} className="mt-4 grid gap-4">
            <fieldset className="grid gap-2">
              <legend className="mb-1 text-sm font-medium text-ink">
                Action
              </legend>
              {choices.map((c) => (
                <label key={c.value} className="inline-flex min-h-11 cursor-pointer items-center gap-3 text-base text-ink">
                  <input
                    type="radio"
                    value={c.value}
                    {...form.register("action")}
                    className="size-5 shrink-0 accent-accent"
                  />
                  <span>{c.label}</span>
                </label>
              ))}
            </fieldset>
            <FormField
              control={form.control}
              name="reason"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Reason for the audit log</FormLabel>
                  <FormControl>
                    <Textarea {...field} rows={3} className="min-h-20 font-sans text-base" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {resolve.isError && (
              <p role="alert" className="text-sm font-medium text-error">
                {resolve.error instanceof ConnectError ? resolve.error.rawMessage : resolve.error.message}
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="submit" pending={resolve.isPending}>
                Resolve report
              </Button>
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
            </div>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
