import { ChevronLeft, ChevronRight } from "lucide-react";
import { Fragment, useEffect, useRef, type ReactNode } from "react";

import { Button } from "@/components/ui/Button";

export type ReaderProps = {
  /** The article's heading: what the reader is reading. */
  title: string;
  /** Shown under the title: author, place, day. */
  byline?: ReactNode;
  /** Part bodies in order; part N is parts[N - 1]. */
  parts: readonly string[];
  /** The current part, 1-based. The caller owns it (the read route keeps it in the URL). */
  part: number;
  onPartChange: (part: number) => void;
};

const FADE_MS = 150;

/**
 * A part body as paragraphs: a blank line separates paragraphs and a single
 * line break inside a paragraph is kept as a line break, the way an author
 * typed it in the composer.
 */
function paragraphs(body: string): string[][] {
  return body
    .split(/\n\s*\n/)
    .map((p) => p.trim())
    .filter((p) => p.length > 0)
    .map((p) => p.split("\n"));
}

/**
 * Keys typed into a field, or into a widget that uses arrow keys itself
 * (tabs, menus, listboxes), belong to that widget and never page the reader.
 */
function ownsArrowKeys(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (target.closest("input, textarea, select, [contenteditable='true']")) return true;
  return target.closest("[role='tablist'], [role='menu'], [role='listbox'], [role='radiogroup'], [role='slider'], [role='dialog']") !== null;
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * One part of a perspective per page, as semantic HTML: an article with the
 * title, the part's paragraphs, and previous and next buttons. ArrowLeft and
 * ArrowRight page while focus is not in a field. A page change moves focus to
 * the new part's heading so a screen reader starts reading it.
 *
 * The cross-fade is the Web Animations API, which the stylesheet's
 * reduced-motion rule cannot reach (it zeroes CSS durations only), so the
 * reader checks prefers-reduced-motion itself and does not animate at all
 * when it is set.
 */
export function Reader({ title, byline, parts, part, onPartChange }: ReaderProps) {
  const total = parts.length;
  const current = Math.min(Math.max(part, 1), Math.max(total, 1));
  const body = parts[current - 1] ?? "";
  const headingRef = useRef<HTMLHeadingElement>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const shown = useRef(current);

  useEffect(() => {
    if (shown.current === current) return;
    shown.current = current;
    headingRef.current?.focus();
    const page = pageRef.current;
    if (page && typeof page.animate === "function" && !prefersReducedMotion()) {
      page.animate([{ opacity: 0 }, { opacity: 1 }], { duration: FADE_MS, easing: "ease-out" });
    }
  }, [current]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (ownsArrowKeys(e.target)) return;
      const next = e.key === "ArrowLeft" ? current - 1 : current + 1;
      if (next < 1 || next > total) return;
      e.preventDefault();
      onPartChange(next);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [current, total, onPartChange]);

  return (
    <article className="mx-auto w-full max-w-measure min-w-0" aria-labelledby="reader-title">
      <header className="border-b border-rule pb-4">
        <h1 id="reader-title" className="text-2xl wrap-anywhere sm:text-3xl">
          {title}
        </h1>
        {byline && <div className="mt-2 text-sm text-ink-muted">{byline}</div>}
      </header>
      <div ref={pageRef} className="py-6">
        <h2 ref={headingRef} tabIndex={-1} className="font-sans text-sm font-medium tracking-wide text-ink-muted uppercase">
          Part {current}
        </h2>
        <div className="prose-body mt-4">
          {paragraphs(body).map((lines, i) => (
            <p key={i}>
              {lines.map((line, j) => (
                <Fragment key={j}>
                  {j > 0 && <br />}
                  {line}
                </Fragment>
              ))}
            </p>
          ))}
        </div>
      </div>
      <nav aria-label="Pages" className="flex items-center justify-between gap-2 border-t border-rule pt-4">
        <Button variant="secondary" disabled={current <= 1} onClick={() => onPartChange(current - 1)} aria-label="Previous part">
          <ChevronLeft aria-hidden size={16} />
          <span className="hidden sm:inline">Previous</span>
        </Button>
        <span aria-live="polite" className="text-sm text-ink-muted">
          Part {current} of {total}
        </span>
        <Button variant="secondary" disabled={current >= total} onClick={() => onPartChange(current + 1)} aria-label="Next part">
          <span className="hidden sm:inline">Next</span>
          <ChevronRight aria-hidden size={16} />
        </Button>
      </nav>
    </article>
  );
}
