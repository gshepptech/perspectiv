import { ShieldAlert } from "lucide-react";
import { Link } from "react-router";

import { Role } from "@/gen/perspectiv/v1/enums_pb";
import { useSession } from "@/lib/session";

/**
 * The band under the header that asks an admin without two-factor sign-in
 * to turn it on. v1 asks and never requires it, so it has no deadline and
 * no close button: it goes once the session the server returns says
 * two-factor sign-in is on.
 */
export function AdminSecurityNotice() {
  const session = useSession();
  if (!session || session.role !== Role.ADMIN || session.twoFactorEnabled) return null;
  return (
    <section aria-label="Account security notice" className="border-b border-rule bg-surface">
      <p className="mx-auto flex w-full max-w-shell flex-wrap items-center gap-x-2 px-4 py-1 text-sm">
        <ShieldAlert aria-hidden size={16} className="shrink-0 text-warning" />
        <span>Your admin account has no two-factor sign-in.</span>
        <Link to="/settings#two-factor" className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Turn it on in Settings
        </Link>
      </p>
    </section>
  );
}
