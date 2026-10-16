import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import { Link } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { emailSchema, passwordSchema, usernameSchema } from "./rules";

const schema = z.object({
  email: emailSchema,
  username: usernameSchema,
  password: passwordSchema,
  // Perspectiv is for adults only: the box must be ticked, and the server
  // rejects a sign-up whose confirmation is false.
  ageConfirmed: z.boolean().refine((v) => v, { error: "You must be 18 or over to sign up." }),
});
type Values = z.infer<typeof schema>;

/** The rules a new member agrees to, shown above the age confirmation. */
function Terms() {
  return (
    <section aria-labelledby="signup-terms" className="rounded-md border border-rule bg-surface p-4 text-sm">
      <h2 id="signup-terms" className="text-base">
        Rules for writing here
      </h2>
      <ul className="mt-2 list-disc space-y-1 pl-5">
        <li>
          You write under your username only. Your email and real name are never shown to anyone.
        </li>
        <li>
          Real names and handles of people who are not users are prohibited. Describe the people in your story as
          characters instead. Anyone can report a perspective that names someone, and moderators remove it.
        </li>
        <li>No harassment, hateful or sexual content, or spam.</li>
      </ul>
    </section>
  );
}

function CheckEmail({ email }: { email: string }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <AuthPanel>
      <AuthHeading ref={heading}>Check your email</AuthHeading>
      <p className="mt-4 break-words">
        We sent a link to <strong className="font-medium">{email}</strong>. Follow it to verify your email. Until then you
        can sign in and read, but not publish, claim, vote, react or report.
      </p>
      <p className="mt-6">
        <Link to="/login" className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}

export function Component() {
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { email: "", username: "", password: "", ageConfirmed: false },
  });
  const signUp = useMutation({
    mutationFn: (values: Values) => clients.auth.signUp(values),
  });

  if (signUp.isSuccess) return <CheckEmail email={signUp.variables.email} />;

  return (
    <AuthPanel>
      <AuthHeading>Sign up</AuthHeading>
      <Form {...form}>
        <form noValidate className="mt-6 grid gap-4" onSubmit={form.handleSubmit((v) => signUp.mutate(v))}>
          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Email</FormLabel>
                <FormControl>
                  <Input type="email" autoComplete="email" {...field} />
                </FormControl>
                <FormDescription>Used to sign in and for notifications. Never shown to anyone.</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Username</FormLabel>
                <FormControl>
                  <Input autoComplete="username" autoCapitalize="none" spellCheck={false} {...field} />
                </FormControl>
                <FormDescription>3 to 20 letters, digits or underscores. This is the only name others see.</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Password</FormLabel>
                <FormControl>
                  <Input type="password" autoComplete="new-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <Terms />
          <FormField
            control={form.control}
            name="ageConfirmed"
            render={({ field }) => (
              <FormItem className="grid-cols-[auto_1fr] items-start gap-x-3">
                <FormControl>
                  <input
                    type="checkbox"
                    name={field.name}
                    ref={field.ref}
                    checked={field.value}
                    onBlur={field.onBlur}
                    onChange={(e) => field.onChange(e.target.checked)}
                    className="mt-0.5 size-5 accent-accent"
                  />
                </FormControl>
                <FormLabel className="text-base font-normal">
                  I am 18 or over, and I agree to the rules above.
                </FormLabel>
                <FormMessage className="col-span-2" />
              </FormItem>
            )}
          />
          <FormAlert message={signUp.error ? errorMessage(signUp.error) : null} />
          <Button type="submit" disabled={signUp.isPending}>
            Sign up
          </Button>
        </form>
      </Form>
      <p className="mt-6 text-sm text-ink-muted">
        Already a member?{" "}
        <Link to="/login" className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}
