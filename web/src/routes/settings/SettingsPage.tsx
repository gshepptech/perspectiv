import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { CircleAlert, CircleCheck } from "lucide-react";

import type { Account } from "@/gen/perspectiv/v1/user_pb";
import { useDocumentTitle } from "@/lib/title";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

import { BlocksSection } from "./BlocksSection";
import { DataSection } from "./DataSection";
import { DeleteAccountSection } from "./DeleteAccountSection";
import { FollowedWritersSection } from "./FollowedWritersSection";
import { NotificationsSection } from "./NotificationsSection";
import { ProfileSection } from "./ProfileSection";
import { myProfileQuery } from "./queries";
import { Section } from "./Section";
import { SecuritySection } from "./SecuritySection";
import { UsernameSection } from "./UsernameSection";

function AccountSection({ account }: { account: Account }) {
  return (
    <Section title="Account">
      <dl className="grid gap-3">
        <div className="min-w-0">
          <dt className="text-sm text-ink-muted">Email</dt>
          <dd className="break-all">{account.email}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-muted">Status</dt>
          <dd className="flex items-center gap-1.5">
            {account.emailVerified ? (
              <>
                <CircleCheck aria-hidden size={16} className="text-success" /> Verified
              </>
            ) : (
              <>
                <CircleAlert aria-hidden size={16} className="text-warning" /> Not verified. Follow the link we emailed you
                to publish, claim, vote and react.
              </>
            )}
          </dd>
        </div>
      </dl>
      <p className="mt-3 text-sm text-ink-muted">Only you see your email.</p>
    </Section>
  );
}

export function Component() {
  useDocumentTitle("Settings");
  const me = useQuery(myProfileQuery);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div className="mx-auto w-full max-w-2xl min-w-0">
      <h1 ref={heading} tabIndex={-1} className="text-2xl focus:outline-none sm:text-3xl">
        Settings
      </h1>
      {me.isPending && (
        <p role="status" className="mt-6 text-ink-muted">
          Loading your settings…
        </p>
      )}
      {me.isError && (
        <div className="mt-6">
          <FormAlert message={errorMessage(me.error)} />
        </div>
      )}
      {me.data?.profile && me.data.account && (
        <div className="mt-6 grid gap-8">
          <AccountSection account={me.data.account} />
          <ProfileSection profile={me.data.profile} />
          <UsernameSection profile={me.data.profile} account={me.data.account} />
          <SecuritySection email={me.data.account.email} />
          <NotificationsSection />
          <FollowedWritersSection />
          <BlocksSection blocked={me.data.account.blocked} />
          <Section title="Your data">
            <DataSection username={me.data.profile.username} />
          </Section>
          <DeleteAccountSection />
        </div>
      )}
    </div>
  );
}
