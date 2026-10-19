import { Link } from "react-router";

/** A username that opens the user's public profile. */
export function AuthorLink({ username }: { username: string }) {
  return (
    <Link to={`/users/${encodeURIComponent(username)}`} className="wrap-anywhere text-accent underline underline-offset-4">
      {username}
    </Link>
  );
}
