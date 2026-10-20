import { BookMarked, CalendarDays, Link2, MapPin, PenLine } from "lucide-react";
import type { ReactNode } from "react";

/**
 * An illustrated example of the product working, for someone who has never
 * seen it: two people at the same place on the same evening, one claiming a
 * character in the other's story, and the linked stories becoming canon. The
 * place, people and prose are invented and the panel is labelled "Example";
 * it is never real content and never a feed (AC-006).
 */
export function ExampleArc() {
  return (
    <figure aria-labelledby="example-caption" className="min-w-0 rounded-2xl bg-accent/8 p-4 sm:p-6">
      <figcaption id="example-caption" className="flex flex-wrap items-center gap-2 text-sm">
        <span className="rounded-full bg-paper px-2.5 py-0.5 font-medium text-accent">Example</span>
        <Chip icon={<MapPin aria-hidden size={14} />}>Moonbean Coffee, Portland</Chip>
        <Chip icon={<CalendarDays aria-hidden size={14} />}>Friday, 9 PM</Chip>
      </figcaption>

      <div className="mt-4 grid min-w-0 gap-4 sm:grid-cols-2">
        <StoryCard author="wren">
          The power went out halfway through open mic. Someone by the window started singing anyway,{" "}
          <mark className="rounded bg-accent/15 px-0.5 text-ink underline decoration-accent decoration-2 underline-offset-4">
            the guy in the yellow raincoat
          </mark>
          , and by the second verse the whole room had joined in.
        </StoryCard>
        <StoryCard author="otter">
          I don’t sing. I don’t know what came over me. It was dark, I still had my raincoat on, and somebody had to fill
          the silence.
        </StoryCard>
      </div>

      <p className="mt-4 flex min-w-0 items-start gap-2 text-sm text-ink">
        <Link2 aria-hidden size={16} className="mt-0.5 shrink-0 text-accent" />
        <span className="min-w-0">
          <strong className="font-medium">otter</strong> claimed “the guy in the yellow raincoat” in wren’s story:{" "}
          <em>that was me.</em>
        </span>
      </p>
      <p className="mt-2 flex min-w-0 items-start gap-2 text-sm text-ink">
        <BookMarked aria-hidden size={16} className="mt-0.5 shrink-0 text-accent" />
        <span className="min-w-0">Linked with two more perspectives into a lore, it became the canon of that night.</span>
      </p>
    </figure>
  );
}

function Chip({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1 rounded-full border border-rule bg-paper px-2.5 py-0.5 text-ink-muted">
      {icon}
      <span className="min-w-0 wrap-anywhere">{children}</span>
    </span>
  );
}

/** One perspective, drawn as a page lifted off a small stack. */
function StoryCard({ author, children }: { author: string; children: ReactNode }) {
  return (
    <div className="relative min-w-0">
      <div aria-hidden className="absolute inset-0 translate-x-1.5 translate-y-1.5 rounded-xl border border-rule bg-surface" />
      <div className="relative h-full rounded-xl border border-rule bg-paper p-4 shadow-sm">
        <p className="text-sm font-medium text-ink-muted">{author}</p>
        <p className="mt-2 font-serif text-prose leading-relaxed text-ink">{children}</p>
      </div>
    </div>
  );
}

/** How Perspectiv works, said as four short notes on the example. */
export function HowItWorks() {
  const notes = [
    { icon: MapPin, text: "Every story is pinned to a place and a day." },
    { icon: PenLine, text: "Everyone who was there writes their own side." },
    { icon: Link2, text: "Spot yourself in someone else’s story and claim the character." },
    { icon: BookMarked, text: "Linked stories become a lore. The one readers love most becomes canon." },
  ];
  return (
    <ul aria-label="How Perspectiv works" className="grid min-w-0 gap-x-6 gap-y-4 sm:grid-cols-2">
      {notes.map(({ icon: Icon, text }) => (
        <li key={text} className="flex min-w-0 items-start gap-3 text-ink">
          <span className="grid size-8 shrink-0 place-items-center rounded-full bg-accent/10 text-accent">
            <Icon aria-hidden size={16} />
          </span>
          <span className="min-w-0 pt-1">{text}</span>
        </li>
      ))}
    </ul>
  );
}
