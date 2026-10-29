import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Textarea } from "@/components/ui/Textarea";
import type { Profile } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

import { myProfileKey } from "./queries";
import { Saved, Section } from "./Section";

// The avatar's size and format limits are the server's to enforce; the file
// picker only narrows the choice to common image types and the server's
// reason is shown when it refuses one.
const ACCEPTED_IMAGES = "image/png,image/jpeg,image/webp";

const schema = z.object({
  bio: z.string(),
  avatar: z.custom<FileList | undefined>(),
  clearAvatar: z.boolean(),
});
type Values = z.infer<typeof schema>;

export function ProfileSection({ profile }: { profile: Profile }) {
  const queryClient = useQueryClient();
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { bio: profile.bio, avatar: undefined, clearAvatar: false },
  });
  const save = useMutation({
    mutationFn: async (v: Values) => {
      const file = v.avatar?.[0];
      if (file) {
        const avatar = new Uint8Array(await file.arrayBuffer());
        return clients.user.updateProfile({ bio: v.bio, avatar, avatarContentType: file.type });
      }
      return clients.user.updateProfile({ bio: v.bio, clearAvatar: v.clearAvatar });
    },
    onSuccess: async (res, v) => {
      form.reset({ bio: res.profile?.bio ?? v.bio, avatar: undefined, clearAvatar: false });
      await queryClient.invalidateQueries({ queryKey: myProfileKey });
    },
  });

  return (
    <Section title="Profile">
      <Form {...form}>
        <form noValidate className="grid gap-4" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
          <div className="flex min-w-0 flex-wrap items-center gap-4">
            {profile.avatarUrl ? (
              <img src={profile.avatarUrl} alt="Your avatar" className="size-16 rounded-full border border-rule object-cover" />
            ) : (
              <p className="text-sm text-ink-muted">No avatar yet.</p>
            )}
          </div>
          <FormField
            control={form.control}
            name="avatar"
            render={({ field: { onChange, onBlur, name, ref } }) => (
              <FormItem>
                <FormLabel>New avatar</FormLabel>
                <FormControl>
                  <input
                    type="file"
                    accept={ACCEPTED_IMAGES}
                    name={name}
                    ref={ref}
                    onBlur={onBlur}
                    onChange={(e) => onChange(e.target.files ?? undefined)}
                    className="block w-full min-w-0 text-sm text-ink file:mr-3 file:min-h-11 file:rounded-md file:border file:border-control file:bg-surface file:px-3 file:text-ink"
                  />
                </FormControl>
                <FormDescription>A PNG, JPEG or WebP image.</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {profile.avatarUrl && (
            <FormField
              control={form.control}
              name="clearAvatar"
              render={({ field }) => (
                <FormItem className="grid-cols-[auto_1fr] items-center gap-x-3">
                  <FormControl>
                    <input
                      type="checkbox"
                      name={field.name}
                      ref={field.ref}
                      checked={field.value}
                      onBlur={field.onBlur}
                      onChange={(e) => field.onChange(e.target.checked)}
                      className="size-5 accent-accent"
                    />
                  </FormControl>
                  <FormLabel className="font-normal">Remove avatar</FormLabel>
                </FormItem>
              )}
            />
          )}
          <FormField
            control={form.control}
            name="bio"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Bio</FormLabel>
                <FormControl>
                  <Textarea className="min-h-24 font-sans text-base" {...field} />
                </FormControl>
                <FormDescription>Shown on your profile. Do not include your real name.</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormAlert message={save.error ? errorMessage(save.error) : null} />
          <Saved message={save.isSuccess && !form.formState.isDirty ? "Profile saved." : null} />
          <div>
            <Button type="submit" pending={save.isPending}>
              Save profile
            </Button>
          </div>
        </form>
      </Form>
    </Section>
  );
}
