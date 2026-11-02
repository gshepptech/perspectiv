import { Code, ConnectError } from "@connectrpc/connect";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowLeft, ChevronLeft, ChevronRight } from "lucide-react";
import { useCallback, useEffect, useRef, type ReactNode } from "react";
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
import { chapterTitles, loreTabTitle, readableStories, UNNAMED } from "./loreText";
import { WhoWasWho } from "./WhoWasWho";

/** The level-one heading of the title, contents and closing pages; a chapter's is the Reader's. */
const HEADING_ID = "book-heading";

/**
 * Every page of the book sits in a frame at least this tall, with its
 * page-turn row at the frame's foot, so the row stays put from page to page
 * (Folio, Lore as a book).
 */
const PAGE_FRAME = "min-h-[calc(100dvh-12rem)]";

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

/**
 * The quiet row above every page: back to the lore, and the contents from a
 * chapter. The closing page carries its own way back, so there the row is
 * empty but keeps its height and the page frame starts where it always does.
 */
function TopBar({ loreId, contents, empty }: { loreId: string; contents: boolean; empty: boolean }) {
  return (
    <p className="mx-auto mb-4 flex min-h-11 max-w-measure flex-wrap items-center gap-x-4">
      {!empty && (
        <Link to={`/lores/${encodeURIComponent(loreId)}`} className={linkClass}>
          <ArrowLeft aria-hidden size={16} />
          Back to the lore
        </Link>
      )}
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
 * the position between them, laid out as a chapter's row is (on a phone the
 * position on its own line above the controls). An end with nowhere to go is
 * a disabled button.
 */
function PageTurn({ previous, next, position }: { previous: BookPage | null; next: BookPage | null; position: string }) {
  return (
    <nav aria-label="Pages" className="mt-8 grid grid-cols-2 items-center gap-2 border-t border-rule pt-4 sm:grid-cols-[1fr_auto_1fr]">
      {previous ? (
        <Button variant="secondary" asChild className="justify-self-start">
          <Link to={{ search: searchOf(previous) }} aria-label="Previous page">
            <ChevronLeft aria-hidden size={16} />
            <span className="hidden sm:inline">Previous</span>
          </Link>
        </Button>
      ) : (
        <Button variant="secondary" disabled aria-label="Previous page" className="justify-self-start">
          <ChevronLeft aria-hidden size={16} />
          <span className="hidden sm:inline">Previous</span>
        </Button>
      )}
      <span className="col-span-2 row-start-1 text-center text-sm text-ink-muted sm:col-span-1 sm:col-start-2">{position}</span>
      {next ? (
        <Button variant="secondary" asChild className="justify-self-end">
          <Link to={{ search: searchOf(next) }} aria-label="Next page">
            <span className="hidden sm:inline">Next</span>
            <ChevronRight aria-hidden size={16} />
          </Link>
        </Button>
      ) : (
        <Button variant="secondary" disabled aria-label="Next page" className="justify-self-end">
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

function ContentsPage({
  chapters,
  titles,
  parts,
  memberIds,
}: {
  chapters: Perspective[];
  titles: readonly string[];
  parts: readonly number[];
  memberIds: Set<string>;
}) {
  return (
    <section aria-labelledby={HEADING_ID} className="mx-auto max-w-measure py-4">
      <Heading>Contents</Heading>
      {chapters.length === 0 ? (
        <p className="mt-4 text-ink-muted">None of this lore’s stories can be read right now.</p>
      ) : (
        <ol aria-label="Chapters" className="mt-6 grid gap-2">
          {chapters.map((p, i) => {
            const n = parts[i] ?? 1;
            const count = `${n} ${n === 1 ? "part" : "parts"}`;
            return (
              <li key={p.id} className="min-w-0">
                {/* The writer's name is plain text: the row is already one link. */}
                <Link
                  to={{ search: searchOf({ kind: "chapter", chapter: i + 1, part: 1 }) }}
                  className="flex min-h-11 min-w-0 flex-col border-b border-rule py-2 hover:bg-surface"
                >
                  <span className="text-sm text-ink-muted">Chapter {i + 1}</span>
                  <span className="min-w-0 font-serif text-xl wrap-anywhere text-accent underline underline-offset-4">{titles[i]}</span>
                  <span className="min-w-0 text-sm wrap-anywhere text-ink-muted">
                    {p.author ? `by ${p.author.username} · ${count}` : count}
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
  // and a press meanwhile, by arrow key or by a chapter's part control, waits
  // for it rather than turning from the page still shown, which would repeat
  // the turn instead of going on. `step` turns from the page on screen; the
  // effect below keeps it current.
  const inFlight = useRef(false);
  const waiting = useRef<boolean[]>([]);
  const step = useRef<(back: boolean) => boolean>(() => false);
  /** The book's one turn path: a page back or forward, or held while a turn lands. True when the press is taken. */
  const press = useCallback((back: boolean) => {
    if (inFlight.current) {
      waiting.current.push(back);
      return true;
    }
    return step.current(back);
  }, []);

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
    step.current = (back: boolean) => {
      const to = back ? previousPage(here, sizes) : nextPage(here, sizes);
      if (!to) return false;
      inFlight.current = true;
      void navigate({ search: searchOf(to) });
      return true;
    };
    // This page is on screen: the turn that led here has landed, and the
    // first press that waited for it turns from here.
    inFlight.current = false;
    const next = waiting.current.shift();
    if (next !== undefined) step.current(next);
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (ownsArrowKeys(e.target)) return;
      if (press(e.key === "ArrowLeft")) e.preventDefault();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [ready, navigate, at, counts, press]);

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
  const titles = chapterTitles(chapters, memberIds);
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
      content = <ContentsPage chapters={chapters} titles={titles} parts={parts} memberIds={memberIds} />;
      break;
    case "end":
      content = <EndPage loreId={lore.id} title={title} />;
      break;
    case "chapter": {
      const p = chapters[page.chapter - 1];
      const genre = p && p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : "";
      content = (
        <Reader
          key={page.chapter}
          chapter={`Chapter ${page.chapter}`}
          title={titles[page.chapter - 1] ?? ""}
          className={PAGE_FRAME}
          byline={
            <p className="flex flex-wrap items-center justify-center gap-x-3">
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
          // Every part control goes through the book's turn path, so a press
          // while a turn lands is held as a key is, and the ends cross chapters.
          onPartChange={(n) => press(n < page.part)}
          beyondStart={{ label: page.chapter > 1 ? "Previous chapter" : "Contents", onTurn: () => press(true) }}
          beyondEnd={{ label: page.chapter < chapters.length ? "Next chapter" : "The end", onTurn: () => press(false) }}
          position={position}
          announcePosition={false}
          arrowKeys={false}
        />
      );
      break;
    }
  }

  return (
    <div className="min-w-0">
      <TopBar loreId={lore.id} contents={page.kind === "chapter"} empty={page.kind === "end"} />
      {/* One status line outside the page that changes, so every turn is announced. */}
      <p role="status" className="sr-only">
        {position}
      </p>
      {page.kind === "chapter" ? (
        content
      ) : (
        <div className={`mx-auto flex max-w-measure flex-col ${PAGE_FRAME}`}>
          <div className="min-w-0">{content}</div>
          <div className="mt-auto">
            <PageTurn previous={previousPage(page, parts)} next={nextPage(page, parts)} position={position} />
          </div>
        </div>
      )}
    </div>
  );
}
