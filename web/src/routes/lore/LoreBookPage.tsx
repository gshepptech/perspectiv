import { Code, ConnectError } from "@connectrpc/connect";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowLeft, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";

import { perspectiveQuery } from "@/components/actions/queries";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { formatDayBeside, genreLabel } from "@/components/reader/format";
import { ownsArrowKeys, Reader } from "@/components/reader/Reader";
import { Button } from "@/components/ui/Button";
import { Genre } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useDocumentTitle } from "@/lib/title";

import {
  type BookPage,
  forgetPlace,
  nextPage,
  pageFrom,
  pageOfPlace,
  positionOf,
  previousPage,
  readPlace,
  searchOf,
  writePlace,
} from "./bookPages";
import { loreTabTitle, readableStories, UNNAMED } from "./loreText";
import { WhoWasWho } from "./WhoWasWho";

/** The level-one heading of the title, contents and closing pages; a chapter's is the Reader's. */
const HEADING_ID = "book-heading";

/** A chapter's title: its number and its writer. */
function chapterTitle(n: number, p: Perspective | undefined): string {
  return `Chapter ${n}: ${p?.author?.username ?? "Someone"}`;
}

function bodiesOf(p: Perspective): string[] {
  return p.parts.toSorted((a, b) => a.position - b.position).map((part) => part.body);
}

function Missing() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="book-missing">
      <h1 id="book-missing" className="text-2xl sm:text-3xl">
        Lore not found
      </h1>
      <p className="mt-3 text-ink-muted">It may have been removed, or the link is wrong.</p>
    </section>
  );
}

const linkClass = "inline-flex min-h-11 items-center gap-1 text-sm text-accent underline underline-offset-4";

/** The quiet row above the pages before the end: back to the lore, and the contents from a chapter. */
function TopBar({ loreId, contents }: { loreId: string; contents: boolean }) {
  return (
    <p className="mx-auto mb-4 flex max-w-measure flex-wrap items-center gap-x-4">
      <Link to={`/lores/${encodeURIComponent(loreId)}`} className={linkClass}>
        <ArrowLeft aria-hidden size={16} />
        Back to the lore
      </Link>
      {contents && (
        <Link to={{ search: searchOf({ kind: "contents" }) }} className={linkClass}>
          Contents
        </Link>
      )}
    </p>
  );
}

/**
 * Previous and next page on the title, contents and closing pages: links to
 * the neighbouring page's address, styled as the Reader's part controls, with
 * the position between them. An end with nowhere to go is a disabled button.
 */
function PageTurn({ previous, next, position }: { previous: BookPage | null; next: BookPage | null; position: string }) {
  return (
    <nav aria-label="Pages" className="mt-8 flex items-center justify-between gap-2 border-t border-rule pt-4">
      {previous ? (
        <Button variant="secondary" asChild>
          <Link to={{ search: searchOf(previous) }} aria-label="Previous page">
            <ChevronLeft aria-hidden size={16} />
            <span className="hidden sm:inline">Previous</span>
          </Link>
        </Button>
      ) : (
        <Button variant="secondary" disabled aria-label="Previous page">
          <ChevronLeft aria-hidden size={16} />
          <span className="hidden sm:inline">Previous</span>
        </Button>
      )}
      <span className="text-sm text-ink-muted">{position}</span>
      {next ? (
        <Button variant="secondary" asChild>
          <Link to={{ search: searchOf(next) }} aria-label="Next page">
            <span className="hidden sm:inline">Next</span>
            <ChevronRight aria-hidden size={16} />
          </Link>
        </Button>
      ) : (
        <Button variant="secondary" disabled aria-label="Next page">
          <span className="hidden sm:inline">Next</span>
          <ChevronRight aria-hidden size={16} />
        </Button>
      )}
    </nav>
  );
}

function Heading({ children, className = "text-2xl sm:text-3xl" }: { children: ReactNode; className?: string }) {
  return (
    <h1 id={HEADING_ID} tabIndex={-1} className={`wrap-anywhere ${className}`}>
      {children}
    </h1>
  );
}

function TitlePage({
  title,
  dayLine,
  writers,
  resume,
  parts,
}: {
  title: string;
  dayLine: string;
  writers: string[];
  resume: BookPage | null;
  parts: readonly number[];
}) {
  return (
    <section aria-labelledby={HEADING_ID} className="mx-auto max-w-measure py-8 text-center">
      <Heading className="text-3xl leading-tight sm:text-4xl">{title}</Heading>
      {dayLine && <p className="mt-3 text-ink-muted wrap-anywhere">{dayLine}</p>}
      {writers.length > 0 && (
        <>
          <p className="mt-8 text-sm text-ink-muted">Written by</p>
          <ul aria-label="Writers" className="flex flex-wrap justify-center gap-x-4">
            {writers.map((w) => (
              <li key={w} className="min-w-0">
                <AuthorLink username={w} className="inline-flex min-h-11 items-center font-serif text-prose" />
              </li>
            ))}
          </ul>
        </>
      )}
      {resume && (
        <div className="mt-8">
          <Button asChild>
            <Link to={{ search: searchOf(resume) }}>Continue where you left off</Link>
          </Button>
          <p className="mt-2 text-sm text-ink-muted">{positionOf(resume, parts)}</p>
        </div>
      )}
    </section>
  );
}

function ContentsPage({ chapters, parts, memberIds }: { chapters: Perspective[]; parts: readonly number[]; memberIds: Set<string> }) {
  return (
    <section aria-labelledby={HEADING_ID} className="mx-auto max-w-measure py-4">
      <Heading>Contents</Heading>
      {chapters.length === 0 ? (
        <p className="mt-4 text-ink-muted">None of this lore’s stories can be read right now.</p>
      ) : (
        <ol aria-label="Chapters" className="mt-6 grid gap-2">
          {chapters.map((p, i) => {
            const n = parts[i] ?? 1;
            return (
              <li key={p.id} className="min-w-0">
                <Link
                  to={{ search: searchOf({ kind: "chapter", chapter: i + 1, part: 1 }) }}
                  className="flex min-h-11 min-w-0 flex-wrap items-baseline gap-x-3 border-b border-rule py-2 hover:bg-surface"
                >
                  <span className="text-sm text-ink-muted">Chapter {i + 1}</span>
                  <span className="min-w-0 font-serif text-xl wrap-anywhere text-accent underline underline-offset-4">
                    {p.author?.username ?? "Someone"}
                  </span>
                  <span className="text-sm text-ink-muted">
                    {n} {n === 1 ? "part" : "parts"}
                  </span>
                </Link>
              </li>
            );
          })}
        </ol>
      )}
      <WhoWasWho stories={chapters} memberIds={memberIds} />
    </section>
  );
}

/** "Previous chapter" and "Next chapter" under a chapter's last part. */
function ChapterEnd({ chapter, chapters }: { chapter: number; chapters: Perspective[] }) {
  const before = chapter > 1 ? chapters[chapter - 2] : undefined;
  const after = chapter < chapters.length ? chapters[chapter] : undefined;
  const to = (page: BookPage) => ({ search: searchOf(page) });
  const cls = "inline-flex min-h-11 max-w-full min-w-0 items-center gap-1 wrap-anywhere";
  return (
    <nav aria-label="Chapters" className="mx-auto mt-6 flex max-w-measure flex-wrap justify-between gap-3">
      <Button variant="secondary" asChild className={cls}>
        <Link to={before ? to({ kind: "chapter", chapter: chapter - 1, part: 1 }) : to({ kind: "contents" })}>
          <ChevronLeft aria-hidden size={16} className="shrink-0" />
          {before ? `Previous chapter: ${before.author?.username ?? "Someone"}` : "Contents"}
        </Link>
      </Button>
      <Button variant="secondary" asChild className={cls}>
        <Link to={after ? to({ kind: "chapter", chapter: chapter + 1, part: 1 }) : to({ kind: "end" })}>
          {after ? `Next chapter: ${after.author?.username ?? "Someone"}` : "The end"}
          <ChevronRight aria-hidden size={16} className="shrink-0" />
        </Link>
      </Button>
    </nav>
  );
}

function EndPage({ loreId, title }: { loreId: string; title: string }) {
  const lore = `/lores/${encodeURIComponent(loreId)}`;
  return (
    <section aria-labelledby={HEADING_ID} className="mx-auto max-w-measure py-8 text-center">
      <Heading>The end</Heading>
      <p className="mt-3 font-serif text-prose wrap-anywhere">You have read every story in {title}.</p>
      <p className="mt-6 flex flex-wrap justify-center gap-x-6">
        <Link to={lore} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Back to the lore
        </Link>
        <Link to={`${lore}#thread`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Read its thread
        </Link>
      </p>
    </section>
  );
}

/** Which page the reader is on, ignoring the part: a turn that changes it focuses the page's level-one heading. */
function pageIdentity(page: BookPage): string {
  return page.kind === "chapter" ? `chapter-${page.chapter}` : page.kind;
}

/**
 * A lore read as a book (docs/design-system.md, Lore as a book): a title
 * page, the contents, one chapter per readable member story in the lore
 * page's order paged by part through the story page's Reader, and a closing
 * page. The page is in the address; the arrow keys turn pages across
 * chapters; the reader's place is kept on this device only. It reads what
 * the lore page reads, GetLore and each member story's Get, from the same
 * cache entries.
 */
export function Component() {
  const { loreId = "" } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const loreQuery = useQuery({
    queryKey: ["lore", "getLore", { loreId }],
    queryFn: ({ signal }) => clients.lore.getLore({ loreId }, { signal }),
    retry: false,
  });
  const lore = loreQuery.data?.lore;
  const stories = useQueries({ queries: (lore?.members ?? []).map((m) => perspectiveQuery(m.perspectiveId)) });
  // Chapters are numbered once every story has answered, so an address's
  // chapter never shifts under the reader as stories arrive.
  const ready = !!lore && stories.every((s) => !s.isPending);
  const chapters = ready ? readableStories(stories) : [];
  const bodies = chapters.map(bodiesOf);
  const parts = bodies.map((b) => Math.max(b.length, 1));
  const page = pageFrom(params, parts);
  const first = chapters[0];
  const dayTitle = first?.arcTitle ?? "";
  const title = lore?.name || UNNAMED;

  useDocumentTitle(loreQuery.isError ? "Lore not found" : lore && ready ? `${loreTabTitle(lore.name, dayTitle)}, as a book` : undefined);

  // A turn changes the address at once but renders later: the router commits
  // its state in a transition. Until the turn is on screen it is in flight,
  // and an arrow key pressed meanwhile waits for it rather than turning from
  // the page still shown, which would repeat the turn instead of going on.
  const inFlight = useRef(false);
  const waiting = useRef<boolean[]>([]);
  const turn = (to: BookPage) => {
    inFlight.current = true;
    void navigate({ search: searchOf(to) });
  };

  // The book turns every page with the arrow keys, parts included (the
  // Reader's own keys are off), so one listener decides each key. The page
  // and the part counts go in as strings, so this effect runs again exactly
  // when another page is on screen.
  const at = searchOf(page);
  const counts = parts.join(",");
  useEffect(() => {
    if (!ready) return;
    const sizes = counts === "" ? [] : counts.split(",").map(Number);
    const here = pageFrom(new URLSearchParams(at), sizes);
    const step = (back: boolean) => {
      const to = back ? previousPage(here, sizes) : nextPage(here, sizes);
      if (!to) return false;
      inFlight.current = true;
      void navigate({ search: searchOf(to) });
      return true;
    };
    // This page is on screen: the turn that led here has landed, and the
    // first key that waited for it turns from here.
    inFlight.current = false;
    const next = waiting.current.shift();
    if (next !== undefined) step(next);
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (ownsArrowKeys(e.target)) return;
      const back = e.key === "ArrowLeft";
      if (inFlight.current) {
        e.preventDefault();
        waiting.current.push(back);
        return;
      }
      if (step(back)) e.preventDefault();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [ready, navigate, at, counts]);

  // After a turn to another page (not another part of the same chapter, which
  // the Reader focuses itself), focus its level-one heading. Arriving at the
  // book focuses nothing.
  const identity = ready ? pageIdentity(page) : "";
  const shown = useRef("");
  useEffect(() => {
    if (!identity) return;
    const before = shown.current;
    shown.current = identity;
    if (before === "" || before === identity) return;
    document.getElementById(identity.startsWith("chapter") ? "reader-title" : HEADING_ID)?.focus();
  }, [identity]);

  // The reader's place, on this device only: a chapter's part is kept by its
  // story's id, and reaching the end forgets it.
  const chapterId = page.kind === "chapter" ? (chapters[page.chapter - 1]?.id ?? "") : "";
  const part = page.kind === "chapter" ? page.part : 0;
  useEffect(() => {
    if (!ready) return;
    if (chapterId) writePlace(loreId, { perspectiveId: chapterId, part });
    else if (page.kind === "end") forgetPlace(loreId);
  }, [ready, loreId, chapterId, part, page.kind]);

  if (loreQuery.isPending || (lore && !ready)) {
    return (
      <p role="status" className="mx-auto max-w-measure text-ink-muted">
        Opening the book…
      </p>
    );
  }
  if (loreQuery.isError) {
    if (ConnectError.from(loreQuery.error).code === Code.NotFound) return <Missing />;
    return (
      <p role="alert" className="mx-auto max-w-measure text-error">
        This lore could not be loaded. Try again in a moment.
      </p>
    );
  }
  if (!lore) return <Missing />;

  const position = positionOf(page, parts);
  const writers = [...new Set(chapters.map((p) => p.author?.username ?? "").filter(Boolean))];
  const memberIds = new Set(lore.members.map((m) => m.user?.userId ?? ""));
  let content: ReactNode;
  switch (page.kind) {
    case "title": {
      // Read on each visit to the title page, so it offers where the reader
      // got to in this visit too.
      const resume = pageOfPlace(readPlace(loreId), chapters.map((p) => p.id), parts);
      const dayLine = [dayTitle, first?.place?.name, first ? formatDayBeside(dayTitle, first.localDate, first.hour) : ""].filter(Boolean).join(" · ");
      content = <TitlePage title={title} dayLine={dayLine} writers={writers} resume={resume} parts={parts} />;
      break;
    }
    case "contents":
      content = <ContentsPage chapters={chapters} parts={parts} memberIds={memberIds} />;
      break;
    case "end":
      content = <EndPage loreId={lore.id} title={title} />;
      break;
    case "chapter": {
      const p = chapters[page.chapter - 1];
      const genre = p && p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : "";
      const total = parts[page.chapter - 1] ?? 1;
      content = (
        <>
          <Reader
            key={page.chapter}
            title={chapterTitle(page.chapter, p)}
            byline={
              <p className="flex flex-wrap items-center gap-x-3">
                {p?.author && (
                  <span className="inline-flex min-h-11 items-center gap-1">
                    by <AuthorLink username={p.author.username} className="inline-flex min-h-11 items-center" />
                  </span>
                )}
                {genre && <span>{genre}</span>}
              </p>
            }
            parts={bodies[page.chapter - 1] ?? []}
            part={page.part}
            onPartChange={(n) => turn({ kind: "chapter", chapter: page.chapter, part: n })}
            position={position}
            announcePosition={false}
            arrowKeys={false}
          />
          {page.part >= total && <ChapterEnd chapter={page.chapter} chapters={chapters} />}
        </>
      );
      break;
    }
  }

  return (
    <div className="min-w-0">
      {/* The closing page carries its own way back to the lore. */}
      {page.kind !== "end" && <TopBar loreId={lore.id} contents={page.kind === "chapter"} />}
      {/* One status line outside the page that changes, so every turn is announced. */}
      <p role="status" className="sr-only">
        {position}
      </p>
      {content}
      {page.kind !== "chapter" && (
        <div className="mx-auto max-w-measure">
          <PageTurn previous={previousPage(page, parts)} next={nextPage(page, parts)} position={position} />
        </div>
      )}
    </div>
  );
}
