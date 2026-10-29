import { Code } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { clearSession } from "@/lib/session";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";
import { passwordSchema } from "@/routes/auth/rules";

import { Section } from "./Section";

const schema = z.object({ password: passwordSchema });
type Values = z.infer<typeof schema>;

/**
 * Account deletion, behind a dialog that asks for the current password. On
 * success the in-memory session and every cached query are dropped and the
 * reader lands on the home page signed out.
 */
export function DeleteAccountSection() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { password: "" } });
  const remove = useMutation({
    mutationFn: (v: Values) => clients.user.deleteAccount(v),
    onSuccess: async () => {
      clearSession();
      queryClient.clear();
      await navigate("/", { replace: true });
    },
  });

  return (
    <Section title="Delete account">
      <p>
        Deleting your account removes it and every perspective you wrote. Claims that link your perspectives to
        others are dissolved. This cannot be undone.
      </p>
      <Dialog
        onOpenChange={(open) => {
          if (!open) {
            form.reset();
            remove.reset();
          }
        }}
      >
        <DialogTrigger asChild>
          <Button variant="danger" className="mt-4">
            Delete account…
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Delete your account</DialogTitle>
          <DialogDescription>Enter your password to confirm. Your perspectives are removed with it.</DialogDescription>
          <Form {...form}>
            <form noValidate className="mt-4 grid gap-4" onSubmit={form.handleSubmit((v) => remove.mutate(v))}>
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
                  remove.error
                    ? errorMessage(remove.error, {
                        [Code.Unauthenticated]: "That password is not right.",
                        [Code.PermissionDenied]: "That password is not right.",
                      })
                    : null
                }
              />
              <Button type="submit" variant="danger" pending={remove.isPending}>
                Delete my account
              </Button>
            </form>
          </Form>
        </DialogContent>
      </Dialog>
    </Section>
  );
}
