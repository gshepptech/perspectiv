import { Link } from "react-router";

import { cn } from "@/lib/cn";

/**
 * The "Read more" link that closes each short summary of how Perspectiv
 * works (the front page's list, a new member's box, the day page's
 * section) and opens the guide. Its name begins with the visible words.
 */
export function ReadMoreLink({ className }: { className?: string }) {
  return (
    <p className={cn("text-sm", className)}>
      <Link
        to="/how-it-works"
        aria-label="Read more about how Perspectiv works"
        className="inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4"
      >
        Read more
      </Link>
    </p>
  );
}
