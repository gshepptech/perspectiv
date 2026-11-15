import type { ReactNode } from "react";
import { Link } from "react-router";

import { useDocumentTitle } from "@/lib/title";

/** When the legal pages last changed; shown on each. */
const LEGAL_UPDATED = "November 2, 2026";

/** A legal or trust page: a title, the date it last changed, and prose at the reading measure. */
export function LegalPage({ title, children }: { title: string; children: ReactNode }) {
  useDocumentTitle(title);
  return (
    <article aria-labelledby="legal-title" className="mx-auto min-w-0 max-w-measure pb-16">
      <h1 id="legal-title" className="text-2xl sm:text-3xl">
        {title}
      </h1>
      <p className="mt-1 text-sm text-ink-muted">Last updated {LEGAL_UPDATED}</p>
      <div className="prose-body mt-6 grid gap-4 [&_h2]:mt-4 [&_h2]:text-xl [&_li]:ml-5 [&_li]:list-disc [&_ul]:grid [&_ul]:gap-1">
        {children}
      </div>
      <nav aria-label="Other policies" className="mt-10 flex flex-wrap gap-x-4 border-t border-rule pt-4 text-sm">
        <Link to="/terms" className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Terms
        </Link>
        <Link to="/privacy" className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Privacy
        </Link>
        <Link to="/guidelines" className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Community guidelines
        </Link>
      </nav>
    </article>
  );
}
