import { ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/Dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Textarea } from "@/components/ui/Textarea";
import type { ModerationAction } from "@/gen/perspectiv/v1/enums_pb";
import type { Report } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";

import { actionLabel, actionsFor, targetKindLabel } from "./labels";

const schema = z.object({
  action: z.string(),
  reason: z.string().refine((r) => r.trim() !== "", "Say why. The reason is written to the audit log."),
});
type Values = z.infer<typeof schema>;

export function ResolveDialog({
  report,
  onClose,
  onResolved,
}: {
  report: Report;
  onClose: () => void;
  onResolved: (action: ModerationAction) => void;
}) {
  const actions = actionsFor(report.targetKind);
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { action: String(actions[0]), reason: "" },
  });
  const resolve = useMutation({
    mutationFn: (v: Values) =>
      clients.moderation.resolve({ reportId: report.id, action: Number(v.action), reason: v.reason.trim() }),
    onSuccess: (_, v) => onResolved(Number(v.action) as ModerationAction),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogTitle>Resolve report</DialogTitle>
        <DialogDescription>
          {targetKindLabel(report.targetKind)}: {report.targetLabel}
        </DialogDescription>
        <Form {...form}>
          <form noValidate onSubmit={form.handleSubmit((v) => resolve.mutate(v))} className="mt-4 grid gap-4">
            <fieldset className="grid gap-2">
              <legend className="mb-1 text-sm font-medium text-ink">
                Action
              </legend>
              {actions.map((a) => (
                <label key={a} className="inline-flex min-h-11 cursor-pointer items-center gap-3 text-base text-ink">
                  <input
                    type="radio"
                    value={String(a)}
                    {...form.register("action")}
                    className="size-5 shrink-0 accent-accent"
                  />
                  <span>{actionLabel(a)}</span>
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
                {ConnectError.from(resolve.error).rawMessage}
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="submit" disabled={resolve.isPending}>
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
