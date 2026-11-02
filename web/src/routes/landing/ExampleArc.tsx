import { BookMarked, Link2 } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@/lib/cn";
import { ReadMoreLink } from "@/routes/guide/ReadMore";

/**
 * An illustrated example of the product working, for someone who has never
 * seen it: two people at the same place on the same evening, one claiming a
 * character in the other's story, and the linked stories becoming canon. It
 * is drawn as a book opened flat, two facing pages split by a hairline
 * gutter (Folio, Landing). The place, people and prose are invented and the
 * figure is labelled "Example"; it is never real content and never a feed
 * (AC-006).
 */
export function ExampleArc() {
  return (
    <figure aria-labelledby="example-caption" className="min-w-0">
      <figcaption id="example-caption" className="text-center text-sm text-ink-muted">
        <span className="font-medium text-ink">Example</span>
        <span aria-hidden> · </span>
        <span className="sr-only">: </span>
        Moonbean Coffee, Portland
        <span aria-hidden> · </span>
        <span className="sr-only">, </span>
        Friday, 9 PM
      </figcaption>

      <div className="mt-4 grid min-w-0 divide-y divide-rule border-y border-rule sm:grid-cols-2 sm:divide-x sm:divide-y-0">
        <Page author="wren" className="sm:pr-8">
          The power went out halfway through open mic. Someone by the window started singing anyway,{" "}
          <mark className="bg-transparent text-ink underline decoration-accent decoration-2 underline-offset-4">
            the guy in the yellow raincoat
          </mark>
          , and by the second verse the whole room had joined in.
        </Page>
        <Page author="otter" className="sm:pl-8">
          I don’t sing. I don’t know what came over me. It was dark, I still had my raincoat on, and somebody had to fill
          the silence.
        </Page>
      </div>

      <p className="mt-4 flex min-w-0 items-start gap-2 text-sm text-ink">
        <Link2 aria-hidden size={16} className="mt-0.5 shrink-0 text-ink-muted" />
        <span className="min-w-0">
          <strong className="font-medium">otter</strong> claimed “the guy in the yellow raincoat” in wren’s story:{" "}
          <em>that was me.</em>
        </span>
      </p>
      <p className="mt-2 flex min-w-0 items-start gap-2 text-sm text-ink">
        <BookMarked aria-hidden size={16} className="mt-0.5 shrink-0 text-ink-muted" />
        <span className="min-w-0">Linked with two more stories into a lore, it became the canon of that night.</span>
      </p>
    </figure>
  );
}

/** One perspective as one of two facing pages: its writer as the running head, then the prose. */
function Page({ author, className, children }: { author: string; className: string; children: ReactNode }) {
  return (
    <div className={cn("min-w-0 py-6", className)}>
      <p className="text-sm text-ink-muted">{author}</p>
      <p className="mt-3 font-serif text-prose text-ink">{children}</p>
    </div>
  );
}

/** How Perspectiv works, said as four numbered lines under the example, then the way into the guide. */
export function HowItWorks() {
  return (
    <section aria-labelledby="landing-how" className="min-w-0">
      <h2 id="landing-how" className="text-xl">
        How Perspectiv works
      </h2>
      <ol aria-labelledby="landing-how" className="mt-3 grid list-decimal gap-2 pl-5 text-ink marker:text-ink-muted">
        <li>Every story is pinned to a place and a day.</li>
        <li>Everyone who was there writes their own side.</li>
        <li>Spot yourself in someone else’s story and claim the character.</li>
        <li>Linked stories become a lore, and the lore the people who were there favour most becomes canon.</li>
      </ol>
      <ReadMoreLink className="mt-2" />
    </section>
  );
}
