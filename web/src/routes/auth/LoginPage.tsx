import { zodResolver } from "@hookform/resolvers/zod";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";
import { signIn } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { anyCodeSchema, appCodeSchema, emailSchema, passwordSchema, safeNext } from "./rules";

const schema = z.object({ email: emailSchema, password: passwordSchema });
type Values = z.infer<typeof schema>;

// The two links under the form are separate 44px targets on a phone.
const targetClass = "inline-flex min-h-11 items-center";
// A text button that reads like the links beside it.
const textButtonClass = cn(linkClass, targetClass, "text-sm");

/** The navigation state a page sets when it sends the reader here signed out. */
export type LoginState = { signedOutEverywhere?: boolean };

const appCodeForm = z.object({ code: appCodeSchema });
const recoveryCodeForm = z.object({ code: anyCodeSchema });
type CodeValues = { code: string };

const WRONG_CODE = "That code is not right.";
const TIMED_OUT = "That sign-in timed out. Enter your password again.";

/**
 * The second step of signing in to an account with two-factor sign-in on:
 * a code from the authenticator app, or a recovery code. The challenge from
 * the password step lives only in the page's state.
 */
function CodeStep({ challenge, onSignedIn, onTimedOut, onStartAgain }: {
  challenge: string;
  onSignedIn: () => void;
  onTimedOut: () => void;
  onStartAgain: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const [recovery, setRecovery] = useState(false);
  const form = useForm<CodeValues>({
    resolver: zodResolver(recovery ? recoveryCodeForm : appCodeForm),
    defaultValues: { code: "" },
  });
  useEffect(() => heading.current?.focus(), []);
  const complete = useMutation({
    mutationFn: async ({ code }: CodeValues) => {
      const session = signIn(await clients.auth.completeTwoFactorLogin({ challenge, code }));
      if (!session) throw new Error("the server answered without a session");
    },
    onSuccess: onSignedIn,
    onError: (err) => {
      const code = ConnectError.from(err).code;
      if (code === Code.Unauthenticated) form.setError("code", { message: WRONG_CODE }, { shouldFocus: true });
      if (code === Code.FailedPrecondition) onTimedOut();
    },
  });
  const failure = complete.error ? ConnectError.from(complete.error).code : null;
  const alert =
    failure === null || failure === Code.Unauthenticated || failure === Code.FailedPrecondition
      ? null
      : errorMessage(complete.error, {
          [Code.ResourceExhausted]: "Too many wrong codes. Two-factor sign-in for this account is paused; try again later.",
        });
  const switchMode = () => {
    setRecovery((r) => !r);
    form.reset({ code: "" });
    complete.reset();
  };
  return (
    <>
      <AuthHeading ref={heading}>Enter your code</AuthHeading>
      <p className="mt-4">
        {recovery
          ? "Enter one of the recovery codes you saved when you turned on two-factor sign-in."
          : "Open your authenticator app and enter the six-digit code for Perspectiv."}
      </p>
      <Form {...form}>
        <form noValidate className="mt-6 grid gap-4" onSubmit={form.handleSubmit((v) => complete.mutate(v))}>
          <FormField
            control={form.control}
            name="code"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{recovery ? "Recovery code" : "Code from your app"}</FormLabel>
                <FormControl>
                  {recovery ? (
                    <Input autoComplete="off" autoCapitalize="none" spellCheck={false} className="font-mono" {...field} />
                  ) : (
                    <Input inputMode="numeric" autoComplete="one-time-code" maxLength={7} className="max-w-40" {...field} />
                  )}
                </FormControl>
                {recovery && <FormDescription>Like abcd-efgh-jkmn-pqrs. Each code works once.</FormDescription>}
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert message={alert} />
          <Button type="submit" pending={complete.isPending}>
            Sign in
          </Button>
        </form>
      </Form>
      <p className="mt-6 flex flex-wrap gap-x-6">
        <button type="button" className={textButtonClass} onClick={switchMode}>
          {recovery ? "Use the code from your app" : "Use a recovery code"}
        </button>
        <button type="button" className={textButtonClass} onClick={onStartAgain}>
          Start again
        </button>
      </p>
    </>
  );
}

export function Component() {
  useDocumentTitle("Sign in");
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const arrived = useLocation().state as LoginState | null;
  // Where the reader was headed goes on to sign-up, so a new member is not
  // dropped on the home page after verifying.
  const next = safeNext(params.get("next"));
  const signUpTo = next === "/" ? "/signup" : `/signup?next=${encodeURIComponent(next)}`;
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "", password: "" } });
  // Set while the password was right and a second factor is due.
  const [challenge, setChallenge] = useState<string | null>(null);
  const [timedOut, setTimedOut] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const login = useMutation({
    mutationFn: async (values: Values) => {
      const res = await clients.auth.login(values);
      if (res.twoFactorChallenge) return res.twoFactorChallenge;
      const session = signIn(res);
      if (!session) throw new Error("the server answered without a session");
      return null;
    },
    onSuccess: (pending) => {
      if (pending) {
        setTimedOut(false);
        setChallenge(pending);
        return;
      }
      void navigate(next, { replace: true });
    },
  });
  const backToPassword = (expired: boolean) => {
    setChallenge(null);
    setTimedOut(expired);
    login.reset();
    form.resetField("password");
    requestAnimationFrame(() => heading.current?.focus());
  };

  if (challenge) {
    return (
      <AuthPanel>
        <CodeStep
          challenge={challenge}
          onSignedIn={() => void navigate(next, { replace: true })}
          onTimedOut={() => backToPassword(true)}
          onStartAgain={() => backToPassword(false)}
        />
      </AuthPanel>
    );
  }

  return (
    <AuthPanel>
      <AuthHeading ref={heading}>Sign in</AuthHeading>
      {arrived?.signedOutEverywhere && (
        <p role="status" className="mt-4 text-sm font-medium text-success">
          You are signed out on every device.
        </p>
      )}
      {timedOut && (
        <p role="status" className="mt-4 text-sm font-medium text-ink">
          {TIMED_OUT}
        </p>
      )}
      <Form {...form}>
        <form noValidate className="mt-6 grid gap-4" onSubmit={form.handleSubmit((v) => login.mutate(v))}>
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
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Password</FormLabel>
                <FormControl>
                  <Input type="password" autoComplete="current-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert
            message={
              login.error
                ? errorMessage(login.error, {
                    [Code.Unauthenticated]: "That email and password do not match an account.",
                    [Code.ResourceExhausted]: "Too many attempts. Sign-in for this account is paused for 15 minutes.",
                  })
                : null
            }
          />
          <Button type="submit" pending={login.isPending}>
            Sign in
          </Button>
        </form>
      </Form>
      <p className="mt-6 text-sm">
        <Link to="/forgot-password" className={cn(linkClass, targetClass)}>
          Forgot your password?
        </Link>
      </p>
      <p className="mt-2 text-sm text-ink-muted">
        New here?{" "}
        <Link to={signUpTo} className={cn(linkClass, targetClass)}>
          Create an account
        </Link>
      </p>
    </AuthPanel>
  );
}
