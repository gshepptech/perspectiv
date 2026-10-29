import { timestampDate } from "@bufbuild/protobuf/wkt";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import type { Account, Profile } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { updateSession } from "@/lib/session";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";
import { usernameSchema } from "@/routes/auth/rules";

import { myProfileKey } from "./queries";
import { Saved, Section } from "./Section";

const schema = z.object({ username: usernameSchema });
type Values = z.infer<typeof schema>;

const longDate = new Intl.DateTimeFormat(undefined, { dateStyle: "long" });

/**
 * The username change. The server allows one change per 30 days and states
 * the next allowed time; while that time is in the future the field is held
 * and the date is shown, so the reader is not invited to try and be refused.
 */
export function UsernameSection({ profile, account, now = Date.now() }: { profile: Profile; account: Account; now?: number }) {
  const queryClient = useQueryClient();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { username: profile.username } });
  const change = useMutation({
    mutationFn: (v: Values) => clients.user.changeUsername(v),
    onSuccess: async (res, v) => {
      const username = res.profile?.username || v.username;
      updateSession({ username });
      form.reset({ username });
      await queryClient.invalidateQueries({ queryKey: myProfileKey });
    },
  });

  const next = account.nextUsernameChangeAt ? timestampDate(account.nextUsernameChangeAt) : null;
  const held = next !== null && next.getTime() > now;

  return (
    <Section title="Username">
      <Form {...form}>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={form.handleSubmit((v) => {
            if (v.username === profile.username) {
              form.setError("username", { message: "That is already your username." });
              return;
            }
            change.mutate(v);
          })}
        >
          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>New username</FormLabel>
                <FormControl>
                  <Input autoComplete="username" autoCapitalize="none" spellCheck={false} disabled={held} {...field} />
                </FormControl>
                <FormDescription>
                  {held && next
                    ? `You can change it again on ${longDate.format(next)}.`
                    : "3 to 20 letters, digits or underscores. You can change it once every 30 days."}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert message={change.error ? errorMessage(change.error) : null} />
          <Saved message={change.isSuccess ? `Your username is now ${change.data.profile?.username || change.variables.username}.` : null} />
          <div>
            <Button type="submit" variant="secondary" disabled={held} pending={change.isPending}>
              Change username
            </Button>
          </div>
        </form>
      </Form>
    </Section>
  );
}
