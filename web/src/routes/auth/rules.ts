import { z } from "zod";

// The username rule the server enforces on sign up and on a change: 3 to 20
// letters, digits or underscores. The client checks it first only to answer
// faster; uniqueness (case-insensitive) is the server's to decide.
export const usernameSchema = z
  .string()
  .trim()
  .regex(/^[A-Za-z0-9_]{3,20}$/, { error: "Use 3 to 20 letters, digits or underscores." });

export const emailSchema = z.string().trim().pipe(z.email({ error: "Enter a valid email address." }));

// What counts as a strong enough password is the server's decision; the client
// only refuses an empty one and shows the server's reason for anything else,
// so the two can never disagree.
export const passwordSchema = z.string().min(1, { error: "Enter a password." });

/**
 * Where to go after signing in. Only a same-site path is followed: a value
 * that is absolute ("https://..."), protocol-relative ("//host") or that a
 * browser would normalise into one ("/\\host") falls back to the home page,
 * so a crafted login link cannot send a reader off the site.
 */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return "/";
  return next;
}
