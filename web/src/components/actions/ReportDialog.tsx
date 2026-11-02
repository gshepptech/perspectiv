import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { Flag } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import { Textarea } from "@/components/ui/Textarea";
import { ReportReason, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { reportReasons, reportReasonsFor } from "./labels";
import { useWriteGate } from "./useWriteGate";

// The reports table stores details without a limit; the form caps them so a
// report stays a note to a moderator rather than a document.
const DETAILS_MAX = 1000;

const DUPLICATE = String(ReportReason.DUPLICATE_PLACE);

const schema = z
  .object({
    // The Select holds the enum number as a string; "" is "nothing chosen yet".
    reason: z.string().refine((v) => reportReasons.some((r) => String(r.value) === v), "Choose a reason"),
    details: z.string().trim().max(DETAILS_MAX, `Keep details under ${DETAILS_MAX} characters`),
  })
  // A duplicate is only actionable once the moderator knows its twin.
  .refine((v) => v.reason !== DUPLICATE || v.details !== "", {
    path: ["details"],
    error: "Say which listing it is the same as",
  });

type Values = z.infer<typeof schema>;

type Props = {
  targetKind: ReportTargetKind;
  targetId: string;
  /** What is being reported, as the dialog title names it: "otter's perspective", "Moonbean Coffee". */
  targetLabel: string;
  triggerLabel?: string;
  /** The trigger starts its row of the text column (Folio, Flush ghost buttons). */
  flush?: boolean;
};

/** Reports a perspective, place, user, thread post or lore name to the moderators, with a reason from the fixed set. */
export function ReportDialog({ targetKind, targetId, targetLabel, triggerLabel = "Report", flush = false }: Props) {
  const { gate } = useWriteGate();
  const [open, setOpen] = useState(false);
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { reason: "", details: "" } });
  const duplicate = form.watch("reason") === DUPLICATE;
  const report = useMutation({
    mutationFn: (v: Values) =>
      clients.moderation.report({ targetKind, targetId, reason: Number(v.reason) as ReportReason, details: v.details }),
  });
  // The form and its "Send report" button go away on success, so focus
  // moves to the line that replaces them instead of falling out of the dialog.
  const sent = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    if (report.isSuccess) sent.current?.focus();
  }, [report.isSuccess]);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next && !gate()) return;
        if (next) {
          form.reset();
          report.reset();
        }
        setOpen(next);
      }}
    >
      <DialogTrigger asChild>
        <Button aria-label={`${triggerLabel}: ${targetLabel}`} variant="ghost" size="sm" flush={flush} className="min-h-11 text-ink-muted">
          <Flag aria-hidden size={16} />
          {triggerLabel}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>Report {targetLabel}</DialogTitle>
        <DialogDescription>
          {targetKind === ReportTargetKind.PLACE
            ? "Moderators review every report. If this place is listed twice, say which listing is the other one."
            : "Moderators review every report. Descriptions may not name real people or the handles of anyone who is not a Perspectiv user."}
        </DialogDescription>
        {report.isSuccess ? (
          <p ref={sent} tabIndex={-1} role="status" className="mt-4 text-sm font-medium text-success">
            Report sent. A moderator will review it.
          </p>
        ) : (
          <Form {...form}>
            <form className="mt-4 grid gap-4" noValidate onSubmit={form.handleSubmit((v) => report.mutate(v))}>
              <FormField
                control={form.control}
                name="reason"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Reason</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger aria-label="Reason">
                          <SelectValue placeholder="Choose a reason" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {reportReasonsFor(targetKind === ReportTargetKind.PLACE).map((r) => (
                          <SelectItem key={r.value} value={String(r.value)}>
                            {r.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="details"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{duplicate ? "Which listing is it the same as?" : "Details (optional)"}</FormLabel>
                    <FormControl>
                      <Textarea
                        className="min-h-24 font-sans text-base"
                        maxLength={DETAILS_MAX}
                        aria-required={duplicate || undefined}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>{duplicate ? "Paste its link or name it." : "What should a moderator look at?"}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {report.isError && (
                <p role="alert" className="text-sm font-medium text-error">
                  {actionError(report.error)}
                </p>
              )}
              <div className="flex justify-end">
                <Button type="submit" pending={report.isPending}>
                  Send report
                </Button>
              </div>
            </form>
          </Form>
        )}
      </DialogContent>
    </Dialog>
  );
}
