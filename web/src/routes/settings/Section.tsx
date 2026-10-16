import { useId, type ReactNode } from "react";

/** One titled block of the settings page; its heading names the region for assistive tech. */
export function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="min-w-0 border-t border-rule pt-6">
      <h2 id={id} className="text-xl">
        {title}
      </h2>
      <div className="mt-4">{children}</div>
    </section>
  );
}

/** A confirmation under a form, announced politely. */
export function Saved({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="status" className="text-sm font-medium text-success">
      {message}
    </p>
  );
}
