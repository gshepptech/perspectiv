import { Link } from "react-router";

import { cn } from "@/lib/cn";

/** A username that opens the user's public profile. */
export function AuthorLink({ username, className }: { username: string; className?: string }) {
  return (
    <Link
      to={`/users/${encodeURIComponent(username)}`}
      className={cn("wrap-anywhere text-accent underline underline-offset-4", className)}
    >
      {username}
    </Link>
  );
}
