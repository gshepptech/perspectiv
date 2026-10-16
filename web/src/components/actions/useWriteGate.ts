import { useLocation, useNavigate } from "react-router";

import { useSession, type Session } from "@/lib/session";

/**
 * Write actions on public pages: a signed-out reader who presses one is sent
 * to /login with the current page in `next`, and the action does nothing
 * else. `gate()` returns the session when there is one.
 */
export function useWriteGate(): { session: Session | null; gate: () => Session | null } {
  const session = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  function gate(): Session | null {
    if (session) return session;
    void navigate(`/login?next=${encodeURIComponent(location.pathname + location.search)}`);
    return null;
  }
  return { session, gate };
}
