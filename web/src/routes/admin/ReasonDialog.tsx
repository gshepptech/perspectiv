import { ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Textarea } from "@/components/ui/Textarea";
import { errorMessage } from "@/routes/auth/errors";

const schema = z.object({
  reason: z.string().refine((r) => r.trim() !== "", "Say why. The reason is written to the audit log."),
});
type Values = z.infer<typeof schema>;

/**
 * An admin action that needs a reason for the audit log: a trigger button,
 * then a dialog naming what is undone. On success the dialog closes without
 * returning focus to its trigger, which usually goes away; the caller moves
 * focus to its status line.
 */
export function ReasonDialog({
  trigger,
  title,
  description,
  confirm,
  act,
  onDone,
}: {
  trigger: string;
  title: string;
  description: string;
  confirm: string;
  act: (reason: string) => Promise<unknown>;
  onDone: () => void;
}) {
  const [open, setOpen] = useState(false);
  const done = useRef(false);
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { reason: "" } });
  const run = useMutation({
    mutationFn: (v: Values) => act(v.reason.trim()),
    onSuccess: () => {
      done.current = true;
      setOpen(false);
      onDone();
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) {
          done.current = false;
          form.reset();
          run.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm" className="min-h-11 justify-self-start">
          {trigger}
        </Button>
      </DialogTrigger>
      <DialogContent
        onCloseAutoFocus={(e) => {
          if (done.current) e.preventDefault();
        }}
      >
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription className="[overflow-wrap:anywhere]">{description}</DialogDescription>
        <Form {...form}>
          <form noValidate onSubmit={form.handleSubmit((v) => run.mutate(v))} className="mt-4 grid gap-4">
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
            {run.isError && (
              <p role="alert" className="text-sm font-medium text-error">
                {run.error instanceof ConnectError ? errorMessage(run.error) : run.error.message}
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="submit" pending={run.isPending}>
                {confirm}
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
