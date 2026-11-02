import { ChevronLeft, ChevronRight } from "lucide-react";
import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from "react";

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
  /** Every part on one page, with no paging controls. */
  whole?: boolean;
  /** Offers the "Read all parts on one page" toggle; left out for a story short enough to show whole anyway. */
  onWholeChange?: (whole: boolean) => void;
  /** The position label between the part controls; "Part 3 of 7" when left out. */
  position?: string;
  /**
   * Whether the position label announces itself (`aria-live`). A caller that
   * remounts the reader between pages, as the lore's book does between
   * chapters, announces from a status line of its own instead, since a live
   * region that has just mounted is not read.
   */
  announcePosition?: boolean;
  /**
   * Whether ArrowLeft and ArrowRight turn the parts. False when the caller
   * turns every page with the arrow keys itself, as the lore's book does
   * across chapters: two listeners would each act on the key.
   */
  arrowKeys?: boolean;
};

const FADE_MS = 150;

/** A story under this many words in all is shown whole: paging it is more tapping than reading. */
export const SHORT_STORY_WORDS = 600;

/** Where a viewer's "Read all parts on one page" choice is kept. */
export const WHOLE_STORY_KEY = "perspectiv.reader.whole";

export function wordCount(parts: readonly string[]): number {
  return parts.reduce((n, body) => n + body.split(/\s+/).filter(Boolean).length, 0);
}

function storedWhole(): boolean {
  try {
    return window.localStorage.getItem(WHOLE_STORY_KEY) === "1";
  } catch {
    // Storage blocked (a private window, site data off): page by default.
    return false;
  }
}

/**
 * Whether a story reads whole on one page: always when it is short, else as
 * the viewer last chose, a choice kept in this browser only.
 */
export function useWholeStory(parts: readonly string[]) {
  const short = wordCount(parts) < SHORT_STORY_WORDS;
  const [chosen, setChosen] = useState(storedWhole);
  const choose = useCallback((next: boolean) => {
    setChosen(next);
    try {
      window.localStorage.setItem(WHOLE_STORY_KEY, next ? "1" : "0");
    } catch {
      // The choice still holds for this page; it just is not remembered.
    }
  }, []);
  return { short, whole: short || chosen, choose };
}

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
export function ownsArrowKeys(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (target.closest("input, textarea, select, [contenteditable='true']")) return true;
  return target.closest("[role='tablist'], [role='menu'], [role='listbox'], [role='radiogroup'], [role='slider'], [role='dialog']") !== null;
}

function Body({ text }: { text: string }) {
  return (
    <div className="prose-body mt-4">
      {paragraphs(text).map((lines, i) => (
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
  );
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * One part of a perspective per page, as semantic HTML: an article with the
 * title, the part's paragraphs, and previous and next buttons. ArrowLeft and
 * ArrowRight page while focus is not in a field. A page change moves focus to
 * the new part's heading so a screen reader starts reading it. With `whole`,
 * every part sits on one page under its own heading and nothing pages.
 *
 * The cross-fade is the Web Animations API, which the stylesheet's
 * reduced-motion rule cannot reach (it zeroes CSS durations only), so the
 * reader checks prefers-reduced-motion itself and does not animate at all
 * when it is set.
 */
export function Reader({
  title,
  byline,
  parts,
  part,
  onPartChange,
  whole = false,
  onWholeChange,
  position,
  announcePosition = true,
  arrowKeys = true,
}: ReaderProps) {
  const total = parts.length;
  const current = Math.min(Math.max(part, 1), Math.max(total, 1));
  const body = parts[current - 1] ?? "";
  const headingRef = useRef<HTMLHeadingElement>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const shown = useRef(current);
  const wholeHeadings = useRef<(HTMLHeadingElement | null)[]>([]);

  // On one page, the part the address names (or the reader was on when they
  // switched) is brought into view by focusing its heading.
  useEffect(() => {
    if (!whole || current <= 1) return;
    wholeHeadings.current[current - 1]?.focus();
  }, [whole, current]);

  useEffect(() => {
    if (whole) return;
    if (shown.current === current) return;
    shown.current = current;
    headingRef.current?.focus();
    const page = pageRef.current;
    if (page && typeof page.animate === "function" && !prefersReducedMotion()) {
      page.animate([{ opacity: 0 }, { opacity: 1 }], { duration: FADE_MS, easing: "ease-out" });
    }
  }, [current, whole]);

  useEffect(() => {
    if (whole || !arrowKeys) return;
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
  }, [current, total, onPartChange, whole, arrowKeys]);

  return (
    <article className="mx-auto w-full max-w-measure min-w-0" aria-labelledby="reader-title">
      <header className="border-b border-rule pb-4">
        {/* tabIndex -1: a caller that turns to a new story (the lore's book) moves focus here. */}
        <h1 id="reader-title" tabIndex={-1} className="text-2xl wrap-anywhere sm:text-3xl">
          {title}
        </h1>
        {byline && <div className="mt-2 text-sm text-ink-muted">{byline}</div>}
        {onWholeChange && (
          <Button variant="ghost" size="sm" className="mt-2 min-h-11 px-2" aria-pressed={whole} onClick={() => onWholeChange(!whole)}>
            <span
              aria-hidden
              className={`inline-block size-4 rounded-sm border ${whole ? "border-accent bg-accent" : "border-control bg-paper"}`}
            />
            Read all parts on one page
          </Button>
        )}
      </header>
      {whole ? (
        <div className="py-6">
          {parts.map((text, i) => (
            <section key={i} aria-labelledby={total > 1 ? `reader-part-${i + 1}` : undefined} className={i > 0 ? "mt-8" : undefined}>
              {total > 1 && (
                <h2
                  id={`reader-part-${i + 1}`}
                  ref={(el) => {
                    wholeHeadings.current[i] = el;
                  }}
                  tabIndex={-1}
                  className="font-sans text-sm font-medium tracking-wide text-ink-muted uppercase"
                >
                  Part {i + 1}
                </h2>
              )}
              <Body text={text} />
            </section>
          ))}
        </div>
      ) : (
        <>
          <div ref={pageRef} className="py-6">
            <h2 ref={headingRef} tabIndex={-1} className="font-sans text-sm font-medium tracking-wide text-ink-muted uppercase">
              Part {current}
            </h2>
            <Body text={body} />
          </div>
          <nav aria-label="Pages" className="flex items-center justify-between gap-2 border-t border-rule pt-4">
            <Button variant="secondary" disabled={current <= 1} onClick={() => onPartChange(current - 1)} aria-label="Previous part">
              <ChevronLeft aria-hidden size={16} />
              <span className="hidden sm:inline">Previous</span>
            </Button>
            <span aria-live={announcePosition ? "polite" : undefined} className="text-sm text-ink-muted">
              {position ?? `Part ${current} of ${total}`}
            </span>
            <Button variant="secondary" disabled={current >= total} onClick={() => onPartChange(current + 1)} aria-label="Next part">
              <span className="hidden sm:inline">Next</span>
              <ChevronRight aria-hidden size={16} />
            </Button>
          </nav>
        </>
      )}
    </article>
  );
}
