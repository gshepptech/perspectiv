import { ChevronLeft, ChevronRight } from "lucide-react";
import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";

/**
 * Somewhere the reader goes past the first or last part, supplied by a caller
 * that has one (the lore's book: the neighbouring chapter). The part control
 * at that end turns there, shown and named by `label`, instead of sitting
 * disabled.
 */
export type ReaderEdge = {
  /** The control's visible text and its name: "Next chapter", "Contents". */
  label: string;
  onTurn: () => void;
};

export type ReaderProps = {
  /** The article's heading: what the reader is reading. */
  title: string;
  /**
   * A chapter number set over the title ("Chapter 2"), which centres the
   * header as a chapter opening (Folio, Lore as a book). The heading's name
   * stays "Chapter 2: <title>".
   */
  chapter?: string;
  /** Extra classes for the article, such as the book's page frame. */
  className?: string;
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
  /** What Previous does on the first part; left out, it is disabled there. */
  beyondStart?: ReaderEdge;
  /** What Next does on the last part; left out, it is disabled there. */
  beyondEnd?: ReaderEdge;
};

const FADE_MS = 150;

/** A part's heading: a book's part number in the serif italic, not a form label (Folio, Pagination). */
const PART_HEADING = "font-serif text-base font-normal text-ink-muted italic";

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
 * Previous or Next in the part row. Inside the parts it turns the part, its
 * word shown from `sm` and its arrow always. At its end (`atEnd`) it does the
 * caller's edge action, or is disabled when the caller gave none. An edge
 * label shows at every width, its arrow from `sm`.
 */
function PartControl({ back = false, atEnd, edge, onTurn }: { back?: boolean; atEnd: boolean; edge?: ReaderEdge; onTurn: () => void }) {
  const Icon = back ? ChevronLeft : ChevronRight;
  const word = back ? "Previous" : "Next";
  const beyond = atEnd ? edge : undefined;
  return (
    <Button
      variant="secondary"
      className={back ? "justify-self-start" : "justify-self-end"}
      disabled={atEnd && !edge}
      onClick={beyond ? beyond.onTurn : onTurn}
      aria-label={beyond ? beyond.label : `${word} part`}
    >
      {back && <Icon aria-hidden size={16} className={cn("shrink-0", beyond && "hidden sm:block")} />}
      {beyond ? <span>{beyond.label}</span> : <span className="hidden sm:inline">{word}</span>}
      {!back && <Icon aria-hidden size={16} className={cn("shrink-0", beyond && "hidden sm:block")} />}
    </Button>
  );
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
  chapter,
  className,
  byline,
  parts,
  part,
  onPartChange,
  whole = false,
  onWholeChange,
  position,
  announcePosition = true,
  arrowKeys = true,
  beyondStart,
  beyondEnd,
}: ReaderProps) {
  const total = parts.length;
  // A caller with edge actions gets labelled controls at the ends, too wide
  // to share a phone's row with the position: there the position takes its
  // own line above the two controls, and from `sm` the row is one line.
  const edges = !!(beyondStart || beyondEnd);
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

  // The part the last key turned to. The router changes the address at once
  // but redraws a moment later, so a second key pressed in between would
  // otherwise turn from the part still on screen and be lost; it turns from
  // here instead, and this catches up when the redraw lands.
  const heading = useRef(current);
  useEffect(() => {
    heading.current = current;
  }, [current]);

  useEffect(() => {
    if (whole || !arrowKeys) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (ownsArrowKeys(e.target)) return;
      const next = e.key === "ArrowLeft" ? heading.current - 1 : heading.current + 1;
      if (next < 1 || next > total) return;
      e.preventDefault();
      heading.current = next;
      onPartChange(next);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [total, onPartChange, whole, arrowKeys]);

  return (
    <article className={cn("mx-auto flex w-full max-w-measure min-w-0 flex-col", className)} aria-labelledby="reader-title">
      <header className={cn("border-b border-rule pb-4", chapter && "pt-10 text-center sm:pt-16")}>
        {/* tabIndex -1: a caller that turns to a new story (the lore's book) moves focus here. */}
        {chapter ? (
          // Named outright: the two lines are blocks, which some readers join as "Chapter 2 : otter".
          <h1 id="reader-title" tabIndex={-1} aria-label={`${chapter}: ${title}`}>
            <span className="block font-sans text-sm font-normal text-ink-muted">{chapter}</span>
            <span className="mt-2 block text-3xl leading-tight wrap-anywhere">{title}</span>
          </h1>
        ) : (
          <h1 id="reader-title" tabIndex={-1} className="text-2xl wrap-anywhere sm:text-3xl">
            {title}
          </h1>
        )}
        {byline && <div className={cn("mt-2 text-sm text-ink-muted", chapter && "flex justify-center")}>{byline}</div>}
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
                  className={PART_HEADING}
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
            <h2 ref={headingRef} tabIndex={-1} className={PART_HEADING}>
              Part {current}
            </h2>
            <Body text={body} />
          </div>
          {/* mt-auto: in a page frame (the book's) the row sits at the frame's foot. */}
          <nav
            aria-label="Pages"
            className={cn(
              "mt-auto grid items-center gap-2 border-t border-rule pt-4",
              edges ? "grid-cols-2 sm:grid-cols-[1fr_auto_1fr]" : "grid-cols-[1fr_auto_1fr]",
            )}
          >
            <PartControl back atEnd={current <= 1} edge={beyondStart} onTurn={() => onPartChange(current - 1)} />
            <span
              aria-live={announcePosition ? "polite" : undefined}
              className={cn("text-center text-sm text-ink-muted", edges && "col-span-2 row-start-1 sm:col-span-1 sm:col-start-2")}
            >
              {position ?? `Part ${current} of ${total}`}
            </span>
            <PartControl atEnd={current >= total} edge={beyondEnd} onTurn={() => onPartChange(current + 1)} />
          </nav>
        </>
      )}
    </article>
  );
}
