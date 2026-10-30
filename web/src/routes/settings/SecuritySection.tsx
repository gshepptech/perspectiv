import { Code } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { clearSession } from "@/lib/session";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";
import { emailSchema, passwordSchema } from "@/routes/auth/rules";

import { Saved, Section } from "./Section";

// A wrong current password comes back PermissionDenied; it is the reader's
// typo, not a lack of rights, so it is worded as one.
const wrongPassword = { [Code.PermissionDenied]: "Your current password is not right." };

const passwordForm = z
  .object({ currentPassword: passwordSchema, newPassword: passwordSchema, repeat: z.string() })
  .refine((v) => v.newPassword === v.repeat, { error: "The two passwords differ.", path: ["repeat"] });
type PasswordValues = z.infer<typeof passwordForm>;

function ChangePassword() {
  const form = useForm<PasswordValues>({
    resolver: zodResolver(passwordForm),
    defaultValues: { currentPassword: "", newPassword: "", repeat: "" },
  });
  const change = useMutation({
    mutationFn: (v: PasswordValues) => clients.auth.changePassword({ currentPassword: v.currentPassword, newPassword: v.newPassword }),
    onSuccess: () => form.reset(),
  });
  return (
    <Form {...form}>
      <form noValidate className="grid gap-4" onSubmit={form.handleSubmit((v) => change.mutate(v))} aria-labelledby="security-password">
        <h3 id="security-password" className="text-lg">
          Change password
        </h3>
        <FormField
          control={form.control}
          name="currentPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Current password</FormLabel>
              <FormControl>
                <Input type="password" autoComplete="current-password" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="newPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>New password</FormLabel>
              <FormControl>
                <Input type="password" autoComplete="new-password" {...field} />
              </FormControl>
              <FormDescription>At least 10 characters. Other devices are signed out when it changes.</FormDescription>
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
        <FormAlert message={change.error ? errorMessage(change.error, wrongPassword) : null} />
        <Saved message={change.isSuccess ? "Your password is changed. Every other device is signed out." : null} />
        <div>
          <Button type="submit" variant="secondary" pending={change.isPending}>
            Change password
          </Button>
        </div>
      </form>
    </Form>
  );
}

const emailForm = z.object({ newEmail: emailSchema, currentPassword: passwordSchema });
type EmailValues = z.infer<typeof emailForm>;

function ChangeEmail({ email }: { email: string }) {
  const form = useForm<EmailValues>({ resolver: zodResolver(emailForm), defaultValues: { newEmail: "", currentPassword: "" } });
  const change = useMutation({
    mutationFn: (v: EmailValues) => clients.auth.requestEmailChange(v),
    onSuccess: () => form.reset(),
  });
  return (
    <Form {...form}>
      <form noValidate className="grid gap-4" onSubmit={form.handleSubmit((v) => change.mutate(v))} aria-labelledby="security-email">
        <h3 id="security-email" className="text-lg">
          Change email
        </h3>
        <FormField
          control={form.control}
          name="newEmail"
          render={({ field }) => (
            <FormItem>
              <FormLabel>New email</FormLabel>
              <FormControl>
                <Input type="email" autoComplete="email" autoCapitalize="none" spellCheck={false} {...field} />
              </FormControl>
              <FormDescription>We email a link to the new address. Your email stays {email} until you follow it.</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="currentPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Current password</FormLabel>
              <FormControl>
                <Input type="password" autoComplete="current-password" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormAlert message={change.error ? errorMessage(change.error, wrongPassword) : null} />
        <Saved
          message={
            change.isSuccess
              ? `Check ${change.variables.newEmail} for a link to confirm it. The link works for 48 hours.`
              : null
          }
        />
        <div>
          <Button type="submit" variant="secondary" pending={change.isPending}>
            Send confirmation link
          </Button>
        </div>
      </form>
    </Form>
  );
}

/**
 * Signs out on every device, this one included, behind a confirmation: it is
 * what a reader does after signing in on a borrowed computer.
 */
function SignOutEverywhere() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const signOut = useMutation({
    mutationFn: () => clients.auth.signOutEverywhere({}),
    onSuccess: async () => {
      clearSession();
      queryClient.clear();
      await navigate("/login", { replace: true });
    },
  });
  return (
    <div className="grid gap-3">
      <h3 className="text-lg">Signed-in devices</h3>
      <p>Signed in somewhere you no longer use? Sign out everywhere, then sign in again here.</p>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) signOut.reset();
        }}
      >
        <DialogTrigger asChild>
          <Button variant="secondary" className="justify-self-start">
            Sign out everywhere…
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Sign out everywhere?</DialogTitle>
          <DialogDescription>Every device signed in to your account is signed out, this one too.</DialogDescription>
          <FormAlert message={signOut.error ? errorMessage(signOut.error) : null} />
          <div className="mt-6 flex flex-wrap justify-end gap-3">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button pending={signOut.isPending} onClick={() => signOut.mutate()}>
              Sign out everywhere
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** Password, email and sessions: the parts of an account that keep it the reader's. */
export function SecuritySection({ email }: { email: string }) {
  return (
    <Section title="Sign-in and security">
      <div className="grid gap-8">
        <ChangePassword />
        <ChangeEmail email={email} />
        <SignOutEverywhere />
      </div>
    </Section>
  );
}
