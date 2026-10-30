import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import { Link, useSearchParams } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { useDocumentTitle } from "@/lib/title";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { passwordSchema } from "./rules";

const schema = z
  .object({ newPassword: passwordSchema, repeat: z.string() })
  .refine((v) => v.newPassword === v.repeat, { error: "The two passwords differ.", path: ["repeat"] });
type Values = z.infer<typeof schema>;

function Done() {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <AuthPanel>
      <AuthHeading ref={heading}>Your password is changed</AuthHeading>
      <p className="mt-4">Sign in with your new password.</p>
      <p className="mt-6">
        <Link to="/login" className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}

const askAgain = (
  <Link to="/forgot-password" className={linkClass}>
    Ask for a new link
  </Link>
);

export function Component() {
  useDocumentTitle("Set a new password");
  const [params] = useSearchParams();
  const token = params.get("token");
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { newPassword: "", repeat: "" } });
  const reset = useMutation({
    mutationFn: (v: Values) => clients.auth.resetPassword({ token: token ?? "", newPassword: v.newPassword }),
  });

  if (!token) {
    return (
      <AuthPanel>
        <AuthHeading>This link no longer works</AuthHeading>
        <p className="mt-4">The reset link is incomplete. {askAgain}.</p>
      </AuthPanel>
    );
  }
  if (reset.isSuccess) return <Done />;

  return (
    <AuthPanel>
      <AuthHeading>Set a new password</AuthHeading>
      <Form {...form}>
        <form noValidate className="mt-6 grid gap-4" onSubmit={form.handleSubmit((v) => reset.mutate(v))}>
          <FormField
            control={form.control}
            name="newPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>New password</FormLabel>
                <FormControl>
                  <Input type="password" autoComplete="new-password" {...field} />
                </FormControl>
                <FormDescription>At least 10 characters; a common password is refused.</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="repeat"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Repeat new password</FormLabel>
                <FormControl>
                  <Input type="password" autoComplete="new-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert message={reset.error ? errorMessage(reset.error) : null} />
          <Button type="submit" pending={reset.isPending}>
            Set new password
          </Button>
        </form>
      </Form>
      <p className="mt-6 text-sm text-ink-muted">Link expired or already used? {askAgain}.</p>
    </AuthPanel>
  );
}
