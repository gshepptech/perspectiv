import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { NotificationKind } from "@/gen/perspectiv/v1/enums_pb";
import type { GetPreferencesResponse } from "@/gen/perspectiv/v1/notify_pb";
import { clients } from "@/lib/connect";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";
import { notificationKinds } from "@/routes/inbox/kinds";

import { preferencesKey, preferencesQuery } from "./queries";
import { Section } from "./Section";

type Toggles = { kind: NotificationKind; inApp: boolean; email: boolean };

/**
 * An in-app and an email toggle for each kind, and the daily-summary switch. A change is saved
 * at once, one kind per request; while it is in flight the box shows the new
 * value, and if the save fails it falls back to what the server holds.
 * A kind the server did not list is shown on, the documented default.
 */
export function NotificationsSection() {
  const queryClient = useQueryClient();
  const prefs = useQuery(preferencesQuery);
  const update = useMutation({
    mutationFn: (t: Toggles) => clients.notify.updatePreferences({ preferences: [t] }),
    onSuccess: (res) => {
      queryClient.setQueryData<GetPreferencesResponse>(preferencesKey, (old) =>
        old ? { ...old, preferences: res.preferences } : old,
      );
    },
  });

  // The daily summary: email for the kinds whose email box is ticked
  // arrives once a day in one message instead of one email each.
  const digest = useMutation({
    mutationFn: (on: boolean) => clients.notify.updatePreferences({ emailDigest: on }),
    onSuccess: (res) => {
      queryClient.setQueryData<GetPreferencesResponse>(preferencesKey, (old) =>
        old ? { ...old, emailDigest: res.emailDigest } : old,
      );
    },
  });
  const digestOn = digest.isPending ? digest.variables : (prefs.data?.emailDigest ?? false);

  function toggles(kind: NotificationKind): Toggles {
    if (update.isPending && update.variables.kind === kind) return update.variables;
    const held = prefs.data?.preferences.find((p) => p.kind === kind);
    return held ? { kind, inApp: held.inApp, email: held.email } : { kind, inApp: true, email: true };
  }

  return (
    <Section title="Notifications">
      <p className="text-sm text-ink-muted">Choose what reaches your inbox here and what is also emailed to you.</p>
      {prefs.isPending && (
        <p role="status" className="mt-4 text-ink-muted">
          Loading notification settings…
        </p>
      )}
      {prefs.isError && <FormAlert message={errorMessage(prefs.error)} />}
      {prefs.isSuccess && (
        <div className="mt-4 overflow-x-auto">
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-rule text-sm text-ink-muted">
                <th scope="col" className="py-2 pr-2 font-medium">
                  When
                </th>
                <th scope="col" className="px-2 py-2 text-center font-medium">
                  In app
                </th>
                <th scope="col" className="py-2 pl-2 text-center font-medium">
                  Email
                </th>
              </tr>
            </thead>
            <tbody>
              {notificationKinds.map(({ kind, label }) => {
                const t = toggles(kind);
                return (
                  <tr key={kind} className="border-b border-rule">
                    <th scope="row" className="py-2 pr-2 font-normal">
                      {label}
                    </th>
                    <td className="px-2 py-2 text-center">
                      <input
                        type="checkbox"
                        aria-label={`${label}: in app`}
                        checked={t.inApp}
                        onChange={(e) => update.mutate({ ...t, inApp: e.target.checked })}
                        className="size-5 accent-accent"
                      />
                    </td>
                    <td className="py-2 pl-2 text-center">
                      <input
                        type="checkbox"
                        aria-label={`${label}: email`}
                        checked={t.email}
                        onChange={(e) => update.mutate({ ...t, email: e.target.checked })}
                        className="size-5 accent-accent"
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {prefs.isSuccess && (
        <label className="mt-4 grid grid-cols-[auto_1fr] items-start gap-x-3">
          <input
            type="checkbox"
            checked={digestOn}
            onChange={(e) => digest.mutate(e.target.checked)}
            className="mt-0.5 size-5 accent-accent"
          />
          <span>
            Send my emails as one daily summary
            <span className="block text-sm text-ink-muted">Instead of an email for each, once a day at most.</span>
          </span>
        </label>
      )}
      <div className="mt-2">
        <FormAlert message={update.isError ? errorMessage(update.error) : digest.isError ? errorMessage(digest.error) : null} />
      </div>
    </Section>
  );
}
