import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { Textarea } from "@/components/ui/Textarea";
import { clients } from "@/lib/connect";

import { FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { emailSchema } from "./rules";

const MAX_NOTE = 300;

const schema = z.object({
  email: emailSchema,
  note: z.string().refine((n) => [...n.trim()].length <= MAX_NOTE, `Keep it to ${MAX_NOTE} characters.`),
});
type Values = z.infer<typeof schema>;

/**
 * "No invite? Join the waitlist": a line under the invite-only sign-up form
 * that opens a short request in place. The server answers the same whether
 * or not the address was already listed.
 */
export function Waitlist() {
  const [open, setOpen] = useState(false);
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "", note: "" } });
  const join = useMutation({ mutationFn: (v: Values) => clients.auth.joinWaitlist({ email: v.email, note: v.note.trim() }) });

  if (join.isSuccess) {
    return (
      <p role="status" className="mt-6 rounded-md border border-success p-3 text-sm text-ink">
        You're on the list. We'll email you when there's a place.
      </p>
    );
  }
  if (!open) {
    return (
      <p className="mt-6 text-sm text-ink-muted">
        No invite?{" "}
        <button type="button" className={linkClass} onClick={() => setOpen(true)}>
          Join the waitlist
        </button>
      </p>
    );
  }
  return (
    <section aria-labelledby="waitlist-title" className="mt-6 rounded-md border border-rule bg-surface p-4">
      <h2 id="waitlist-title" className="text-base">
        Join the waitlist
      </h2>
      <Form {...form}>
        <form noValidate className="mt-3 grid gap-3" onSubmit={form.handleSubmit((v) => join.mutate(v))}>
          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Email</FormLabel>
                <FormControl>
                  <Input type="email" autoComplete="email" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="note"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Where would you write about? (optional)</FormLabel>
                <FormControl>
                  <Textarea rows={2} maxLength={MAX_NOTE} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert message={join.error ? errorMessage(join.error) : null} />
          <Button type="submit" variant="secondary" disabled={join.isPending}>
            Request an invite
          </Button>
        </form>
      </Form>
    </section>
  );
}
