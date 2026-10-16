import { forwardRef, type ReactNode } from "react";

/**
 * The column every account form sits in: a single narrow column under the
 * shell's gutter, full width on a phone.
 */
export function AuthPanel({ children }: { children: ReactNode }) {
  return <section className="mx-auto w-full max-w-md min-w-0">{children}</section>;
}

/** The page heading. Focusable so a page can move focus to it after a step completes. */
export const AuthHeading = forwardRef<HTMLHeadingElement, { children: ReactNode }>(function AuthHeading({ children }, ref) {
  return (
    <h1 ref={ref} tabIndex={-1} className="text-2xl focus:outline-none sm:text-3xl">
      {children}
    </h1>
  );
});

/** A request failure under a form, announced when it appears. */
export function FormAlert({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-md border border-error px-3 py-2 text-sm font-medium text-error">
      {message}
    </p>
  );
}

export const linkClass = "text-accent underline underline-offset-4";
