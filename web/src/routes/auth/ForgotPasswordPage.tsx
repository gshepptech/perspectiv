import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import { Link } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { emailSchema } from "./rules";

const schema = z.object({ email: emailSchema });
type Values = z.infer<typeof schema>;

function Sent({ email }: { email: string }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <AuthPanel>
      <AuthHeading ref={heading}>Check your email</AuthHeading>
      {/* The same words whether or not the address has an account, so the page cannot be used to find accounts. */}
      <p className="mt-4 break-words">
        If an account uses {email}, we sent it a link to set a new password. The link works once.
      </p>
      <p className="mt-6">
        <Link to="/login" className={linkClass}>
          Back to sign in
        </Link>
      </p>
    </AuthPanel>
  );
}

export function Component() {
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "" } });
  const request = useMutation({ mutationFn: (v: Values) => clients.auth.requestPasswordReset(v) });

  if (request.isSuccess) return <Sent email={request.variables.email} />;

  return (
    <AuthPanel>
      <AuthHeading>Reset your password</AuthHeading>
      <p className="mt-2 text-ink-muted">We will email you a link to set a new one.</p>
      <Form {...form}>
        <form noValidate className="mt-6 grid gap-4" onSubmit={form.handleSubmit((v) => request.mutate(v))}>
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
          <FormAlert message={request.error ? errorMessage(request.error) : null} />
          <Button type="submit" pending={request.isPending}>
            Send link
          </Button>
        </form>
      </Form>
    </AuthPanel>
  );
}
