import { BookOpen, Inbox, LogOut, PenLine, Settings, ShieldAlert, Users } from "lucide-react";
import { Link, NavLink, Outlet, useNavigate, useNavigation } from "react-router";

import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/DropdownMenu";
import { Role } from "@/gen/perspectiv/v1/enums_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";
import { clearSession, useSession } from "@/lib/session";

const navLink = ({ isActive }: { isActive: boolean }) =>
  cn(
    "inline-flex min-h-11 items-center gap-1.5 border-b-2 px-2 text-sm font-medium",
    isActive ? "border-accent text-ink" : "border-transparent text-ink-muted hover:text-ink",
  );

function AccountMenu({ username, isAdmin }: { username: string; isAdmin: boolean }) {
  const navigate = useNavigate();
  async function signOut() {
    try {
      await clients.auth.logout({});
    } finally {
      // The in-memory token goes whether or not the server answered; the
      // refresh cookie is the server's to revoke.
      clearSession();
      await navigate("/");
    }
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="md" className="min-w-0 px-2" aria-label={`Account: ${username}`}>
          <Users aria-hidden size={20} />
          <span className="hidden max-w-32 truncate sm:inline">{username}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuLabel>Signed in as {username}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => void navigate("/settings")}>
          <Settings aria-hidden size={16} /> Settings
        </DropdownMenuItem>
        {isAdmin && (
          <DropdownMenuItem onSelect={() => void navigate("/admin/reports")}>
            <ShieldAlert aria-hidden size={16} /> Reports
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => void signOut()}>
          <LogOut aria-hidden size={16} /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * The app shell: skip link, one-row header, the routed page in <main> and a
 * one-line footer. Every page inherits the 16px side gutter (px-4) and the
 * shell width cap from here, and nothing in the shell is wider than the
 * viewport, so a route only has to keep its own content within its column.
 */
export function RootLayout() {
  const session = useSession();
  const navigation = useNavigation();
  return (
    <div className="flex min-h-dvh flex-col">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-4 focus:z-50 focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-accent-ink"
      >
        Skip to content
      </a>
      <header className="border-b border-rule">
        <div className="mx-auto flex w-full max-w-shell flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-2">
          <Link to="/" className="inline-flex min-h-11 items-center gap-2 font-serif text-xl font-semibold text-ink">
            <BookOpen aria-hidden size={20} className="text-accent" />
            Perspectiv
          </Link>
          <nav aria-label="Primary" className="flex min-w-0 flex-wrap items-center gap-1">
            {session ? (
              <>
                <NavLink to="/compose" className={navLink}>
                  <PenLine aria-hidden size={20} />
                  <span className="sr-only sm:not-sr-only">Write</span>
                </NavLink>
                <NavLink to="/claims" className={navLink}>
                  <Users aria-hidden size={20} />
                  <span className="sr-only sm:not-sr-only">Claims</span>
                </NavLink>
                <NavLink to="/inbox" className={navLink}>
                  <Inbox aria-hidden size={20} />
                  <span className="sr-only sm:not-sr-only">Inbox</span>
                </NavLink>
                <AccountMenu username={session.username} isAdmin={session.role === Role.ADMIN} />
              </>
            ) : (
              <>
                <NavLink to="/login" className={navLink}>
                  Sign in
                </NavLink>
                <Button asChild size="sm">
                  <Link to="/signup">Sign up</Link>
                </Button>
              </>
            )}
          </nav>
        </div>
      </header>
      <main
        id="main"
        tabIndex={-1}
        aria-busy={navigation.state === "loading"}
        className="mx-auto w-full max-w-shell min-w-0 flex-1 px-4 py-6 focus:outline-none"
      >
        <Outlet />
      </main>
      <footer className="border-t border-rule">
        <p className="mx-auto w-full max-w-shell px-4 py-4 text-sm text-ink-muted">
          Perspectiv. Stories from the people who were there.
        </p>
      </footer>
    </div>
  );
}

export function ShellFallback() {
  return (
    <p role="status" className="px-4 py-6 text-ink-muted">
      Opening Perspectiv…
    </p>
  );
}
