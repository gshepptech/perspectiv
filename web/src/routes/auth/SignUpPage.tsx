import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import { Link, useSearchParams } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { signIn } from "@/lib/session";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { rememberAfterVerify } from "./afterVerify";
import { emailSchema, passwordSchema, safeNext, usernameSchema } from "./rules";
import { Honeypot } from "./Honeypot";
import { Waitlist } from "./Waitlist";

const schema = z.object({
  // Required only while sign-up is invite-only; checked on submit.
  accessCode: z.string(),
  website: z.string(),
  email: emailSchema,
  username: usernameSchema,
  password: passwordSchema,
  // Perspectiv is for adults only: the box must be ticked, and the server
  // rejects a sign-up whose confirmation is false.
  ageConfirmed: z.boolean().refine((v) => v, { error: "You must be 18 or over to sign up." }),
});
type Values = z.infer<typeof schema>;

/**
 * A refused sign-up. During the invite-only beta the server explains a code
 * it would not take; that refusal is shown as written.
 */
function signUpError(err: unknown): string {
  const e = ConnectError.from(err);
  if (e.code === Code.PermissionDenied && e.rawMessage) return e.rawMessage;
  return errorMessage(err);
}

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
      <p className="mt-2">
        The full <Link to="/terms" className={linkClass}>terms</Link>, <Link to="/privacy" className={linkClass}>privacy notice</Link> and{" "}
        <Link to="/guidelines" className={linkClass}>community guidelines</Link> apply.
      </p>
    </section>
  );
}

/** The sign-in page, carrying where the reader was headed. */
function signInHref(next: string): string {
  return next === "/" ? "/login" : `/login?next=${encodeURIComponent(next)}`;
}

function CheckEmail({ email, next }: { email: string; next: string }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <AuthPanel>
      <AuthHeading ref={heading}>Check your email</AuthHeading>
      <p className="mt-4 break-words">
        You are signed in. We sent a link to <strong className="font-medium">{email}</strong>: follow it to verify your
        email. Until then you can read, but not publish, claim, vote, react or report.
      </p>
      <p className="mt-6">
        <Link to={next} className={linkClass}>
          {next === "/" ? "Start reading" : "Continue where you were"}
        </Link>
      </p>
    </AuthPanel>
  );
}

export function Component() {
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const info = useQuery({
    queryKey: ["auth", "getSignUpInfo"],
    queryFn: ({ signal }) => clients.auth.getSignUpInfo({}, { signal }),
  });
  const inviteOnly = info.data?.inviteOnly ?? false;
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { accessCode: params.get("code") ?? "", website: "", email: "", username: "", password: "", ageConfirmed: false },
  });
  const signUp = useMutation({
    mutationFn: (values: Values) => clients.auth.signUp({ ...values, accessCode: inviteOnly ? values.accessCode.trim() : "" }),
    // The new member is signed in at once. The verification link opens a
    // new tab; it picks up from here.
    onSuccess: (res) => {
      signIn(res);
      rememberAfterVerify(next);
    },
  });
  const submit = form.handleSubmit((v) => {
    if (inviteOnly && v.accessCode.trim() === "") {
      form.setError("accessCode", { message: "Enter your invite code." }, { shouldFocus: true });
      return;
    }
    signUp.mutate(v);
  });

  if (signUp.isSuccess) return <CheckEmail email={signUp.variables.email} next={next} />;

  return (
    <AuthPanel>
      <AuthHeading>Sign up</AuthHeading>
      {inviteOnly && <p className="mt-2 text-ink-muted">Perspectiv is in an invite-only beta. Anyone can read; writing needs an account.</p>}
      <Form {...form}>
        <form noValidate className="relative mt-6 grid gap-4" onSubmit={submit}>
          <Honeypot field={form.register("website")} />
          {inviteOnly && (
            <FormField
              control={form.control}
              name="accessCode"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Invite code</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" autoCapitalize="none" spellCheck={false} {...field} />
                  </FormControl>
                  <FormDescription>From a member's invite link or a beta invite.</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
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
                <FormDescription>At least 10 characters; a common password is refused.</FormDescription>
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
                  I am 18 or over, and I agree to the rules and terms above.
                </FormLabel>
                <FormMessage className="col-span-2" />
              </FormItem>
            )}
          />
          <FormAlert message={signUp.error ? signUpError(signUp.error) : null} />
          <Button type="submit" pending={signUp.isPending}>
            Sign up
          </Button>
        </form>
      </Form>
      {inviteOnly && <Waitlist />}
      <p className="mt-6 text-sm text-ink-muted">
        Already a member?{" "}
        <Link to={signInHref(next)} className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}
