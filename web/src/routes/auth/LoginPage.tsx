import { zodResolver } from "@hookform/resolvers/zod";
import { Code } from "@connectrpc/connect";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { Link, useNavigate, useSearchParams } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { signIn } from "@/lib/session";

import { AuthHeading, AuthPanel, FormAlert, linkClass } from "./AuthPanel";
import { errorMessage } from "./errors";
import { emailSchema, passwordSchema, safeNext } from "./rules";

const schema = z.object({ email: emailSchema, password: passwordSchema });
type Values = z.infer<typeof schema>;

export function Component() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "", password: "" } });
  const login = useMutation({
    mutationFn: async (values: Values) => {
      const session = signIn(await clients.auth.login(values));
      if (!session) throw new Error("the server answered without a session");
    },
    onSuccess: () => navigate(safeNext(params.get("next")), { replace: true }),
  });

  return (
    <AuthPanel>
      <AuthHeading>Sign in</AuthHeading>
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
        <Link to="/forgot-password" className={linkClass}>
          Forgot your password?
        </Link>
      </p>
      <p className="mt-2 text-sm text-ink-muted">
        New here?{" "}
        <Link to="/signup" className={linkClass}>
          Create an account
        </Link>
      </p>
    </AuthPanel>
  );
}
