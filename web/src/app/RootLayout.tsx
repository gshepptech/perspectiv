import { Bookmark, BookOpen, CircleUser, Inbox, KeyRound, LogOut, PenLine, ScrollText, Settings, ShieldAlert, User, UserCheck, UserSearch } from "lucide-react";
import { useRef } from "react";
import { Link, NavLink, Outlet, useNavigate, useNavigation } from "react-router";

import { HeaderSearch } from "@/components/search/HeaderSearch";
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
import { useWaitingRequests } from "@/routes/claims/waiting";
import { useUnreadCount } from "@/routes/inbox/unread";

import { AdminSecurityNotice } from "./AdminSecurityNotice";

// On a phone the word sits under the icon, so every item keeps a visible
// label at 375px; from sm it sits beside the icon.
const navItem = "inline-flex min-h-11 min-w-11 flex-col items-center justify-center gap-0.5 px-2 text-xs font-medium sm:flex-row sm:gap-1.5 sm:text-sm";

const navLink = ({ isActive }: { isActive: boolean }) =>
  cn(navItem, "border-b-2", isActive ? "border-accent text-ink" : "border-transparent text-ink-muted hover:text-ink");

/** The badge text for a count: the count itself, or "99+" above 99. */
const badgeText = (count: number, more = false) => (more || count > 99 ? "99+" : String(count));

/**
 * A navigation item's icon with its count as a badge. It follows the word and
 * a space in the DOM and is moved before the word with `order-first`, so the
 * visible text reads "Inbox 2", in the order the link's name says it ("Inbox,
 * 2 unread"): a speech-input user who says what they see names the link
 * (WCAG 2.5.3).
 *
 * The badge hangs 8px off the icon's right edge and grows leftward over it.
 * From sm, where the word follows the icon, a badged icon keeps a 6px right
 * margin on top of the item's 6px gap, so the badge ends 4px short of the
 * word; below sm the word sits under the icon, clear of the badge.
 */
function BadgedIcon({ icon: Icon, badge }: { icon: typeof Inbox; badge: string | null }) {
  return (
    <span className={cn("relative order-first", badge && "sm:mr-1.5")}>
      <Icon aria-hidden size={20} />
      {badge && (
        <span
          aria-hidden
          className="absolute -top-1.5 -right-2 min-w-4.5 rounded-full bg-accent px-1 text-center text-xs leading-4.5 font-bold text-accent-ink"
        >
          {badge}
        </span>
      )}
    </span>
  );
}

/** The Inbox item, with the unread count as a badge and in its name. */
function InboxLink() {
  const unread = useUnreadCount(true).data ?? 0;
  const badge = unread > 0 ? badgeText(unread) : null;
  return (
    <NavLink to="/inbox" className={navLink} aria-label={badge ? `Inbox, ${badge} unread` : undefined}>
      <span>Inbox</span>{" "}
      <BadgedIcon icon={Inbox} badge={badge} />
    </NavLink>
  );
}

/** The Requests item, with the requests waiting on the member as a badge and in its name. */
function RequestsLink() {
  const waiting = useWaitingRequests(true).data;
  const count = waiting?.count ?? 0;
  const badge = count > 0 ? badgeText(count, waiting?.more) : null;
  return (
    <NavLink to="/claims" className={navLink} aria-label={badge ? `Requests, ${badge} waiting` : undefined}>
      <span>Requests</span>{" "}
      <BadgedIcon icon={UserCheck} badge={badge} />
    </NavLink>
  );
}

function AccountMenu({ username, isAdmin }: { username: string; isAdmin: boolean }) {
  const navigate = useNavigate();
  // A page reached from the menu moves focus to its own heading; the menu
  // must not then pull focus back to its button as it closes.
  const went = useRef(false);
  const go = (path: string) => {
    went.current = true;
    void navigate(path);
  };
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
        <button type="button" className={cn(navItem, "min-w-0 rounded-md text-ink-muted hover:text-ink")} aria-label={`Account: ${username}`}>
          <CircleUser aria-hidden size={20} />
          <span className="sm:hidden">Account</span>
          <span className="hidden max-w-32 truncate sm:inline">{username}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        onCloseAutoFocus={(e) => {
          if (went.current) e.preventDefault();
          went.current = false;
        }}
      >
        <DropdownMenuLabel>Signed in as {username}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => go(`/users/${encodeURIComponent(username)}`)}>
          <User aria-hidden size={16} /> Your profile
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => go("/saved")}>
          <Bookmark aria-hidden size={16} /> Saved
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => go("/settings")}>
          <Settings aria-hidden size={16} /> Settings
        </DropdownMenuItem>
        {isAdmin && (
          <DropdownMenuItem onSelect={() => go("/admin/reports")}>
            <ShieldAlert aria-hidden size={16} /> Reports
          </DropdownMenuItem>
        )}
        {isAdmin && (
          <DropdownMenuItem onSelect={() => go("/admin/access")}>
            <KeyRound aria-hidden size={16} /> Beta access
          </DropdownMenuItem>
        )}
        {isAdmin && (
          <DropdownMenuItem onSelect={() => go("/admin/users")}>
            <UserSearch aria-hidden size={16} /> Look up a user
          </DropdownMenuItem>
        )}
        {isAdmin && (
          <DropdownMenuItem onSelect={() => go("/admin/audit")}>
            <ScrollText aria-hidden size={16} /> Audit log
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
            <HeaderSearch className={cn(navItem, "rounded-md text-ink-muted hover:text-ink")} />
            {session ? (
              <>
                <NavLink to="/compose" className={navLink}>
                  <PenLine aria-hidden size={20} />
                  <span>Write</span>
                </NavLink>
                <RequestsLink />
                <InboxLink />
                <AccountMenu username={session.username} isAdmin={session.role === Role.ADMIN} />
              </>
            ) : (
              <>
                <NavLink to="/login" className={navLink}>
                  Sign in
                </NavLink>
                <Button asChild size="sm" className="min-h-11">
                  <Link to="/signup">Sign up</Link>
                </Button>
              </>
            )}
          </nav>
        </div>
      </header>
      <AdminSecurityNotice />
      <main
        id="main"
        tabIndex={-1}
        aria-busy={navigation.state === "loading"}
        className="mx-auto w-full max-w-shell min-w-0 flex-1 px-4 py-6 focus:outline-none"
      >
        <Outlet />
      </main>
      <footer className="border-t border-rule">
        <div className="mx-auto flex w-full max-w-shell flex-wrap items-center justify-between gap-x-6 gap-y-1 px-4 py-2 text-sm text-ink-muted">
          <p>Perspectiv. Stories from the people who were there.</p>
          <nav aria-label="About this site" className="flex flex-wrap gap-x-4">
            <Link to="/how-it-works" className="inline-flex min-h-11 items-center underline-offset-4 hover:text-ink hover:underline">
              How it works
            </Link>
            <Link to="/terms" className="inline-flex min-h-11 items-center underline-offset-4 hover:text-ink hover:underline">
              Terms
            </Link>
            <Link to="/privacy" className="inline-flex min-h-11 items-center underline-offset-4 hover:text-ink hover:underline">
              Privacy
            </Link>
            <Link to="/guidelines" className="inline-flex min-h-11 items-center underline-offset-4 hover:text-ink hover:underline">
              Community guidelines
            </Link>
            <Link to="/appearance" className="inline-flex min-h-11 items-center underline-offset-4 hover:text-ink hover:underline">
              Appearance
            </Link>
          </nav>
        </div>
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
