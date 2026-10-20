# Folio

Folio is the design system for Perspectiv. Perspectiv reads like a novel, not a
feed: a perspective is a short book, each part is a page, and the chrome around
it stays quiet. This file is the output of the design pass. The project
`CLAUDE.md` names it, and where it and component code disagree, this file wins.

The tokens below live in `web/src/app/styles.css`. Component code uses the
Tailwind utilities those tokens generate (`bg-paper`, `text-ink`,
`font-serif`, `max-w-measure`) and never a raw hex value.

## Principles

1. **The page is the product.** Prose gets the width, the type and the
   contrast; navigation and actions sit at the edges.
2. **One thing per page.** A reading page shows one part of one perspective.
   There is no infinite scroll of stories.
3. **Paper, not glass.** Flat surfaces separated by hairline rules. No
   gradients, no shadows deeper than a 1px border, no blur.
4. **Calm motion.** Nothing on a reading surface moves unless the reader asked
   it to, and reduced motion means none at all.

## Type

| Role | Face | Token | Fallback stack |
|---|---|---|---|
| Prose, titles, place names | Literata (variable, self-hosted) | `--font-serif` | `Georgia, "Times New Roman", serif` |
| Interface: buttons, labels, navigation, metadata | Public Sans (variable, self-hosted) | `--font-sans` | `system-ui, -apple-system, "Segoe UI", sans-serif` |

Literata was drawn for long-form reading on screens and keeps its texture at
phone sizes. Public Sans is a neutral grotesque that stays out of the prose's
way. Both are bundled from `@fontsource-variable/*` packages into the build;
the app MUST NOT load fonts from a third-party host at runtime (the only
runtime third party is the email provider).

Scale (rem at a 16px root):

| Step | Size | Line height | Use |
|---|---|---|---|
| `text-sm` | 0.875 | 1.43 | metadata, helper text, timestamps |
| `text-base` | 1 | 1.5 | interface copy, form fields |
| `text-prose` | 1.125 | 1.7 | reading body (serif) |
| `text-xl` | 1.25 | 1.4 | section headings |
| `text-2xl` | 1.5 | 1.33 | page titles on phone |
| `text-3xl` | 1.875 | 1.2 | page titles from the `sm` breakpoint |

Headings use Literata at weight 600. Interface text uses Public Sans at 400 and
500 for emphasis; bold (700) is reserved for counts that need to be scanned.
Running prose is left-aligned, never justified, with `hyphens: auto`.

## Measure and layout

- **Reading measure:** prose columns are capped at `--measure: 65ch` of the
  serif face (about 62 to 70 characters a line).
- **Interface width:** lists, forms and the arc page are capped at
  `--width-shell: 72rem`.
- **Gutter:** every page has a 16px side gutter (`px-4`) at every width. No
  element sets a width larger than its container, and the shell clips nothing:
  a page that scrolls horizontally at 375px wide is a defect.
- **Shell:** a single-row header (wordmark left, primary navigation and the
  account menu right), then `<main id="main">`, then a one-line footer. A skip
  link to `#main` is the first focusable element.
- **Breakpoints:** Tailwind defaults. Layout is single column below `md`
  (768px); from `md` the arc and place pages MAY use a two-column layout whose
  reading column keeps the measure.
- **Spacing:** the 4px Tailwind spacing scale. Vertical rhythm between blocks
  of prose is one line (`1.7em`).

## Palette

Two themes follow the operating system's `prefers-color-scheme`: **Paper**
(light) and **Lamplight** (dark). Every text pairing meets WCAG 2.1 AA; the
ratios below are computed against `paper` and `surface`, and the unit test in
`web/src/app/tokens.test.ts` recomputes them from the shipped stylesheet.

| Token | Paper | Lamplight | Use | Ratio on paper / surface (light, dark) |
|---|---|---|---|---|
| `paper` | `#F7F5F0` | `#161514` | page background | |
| `surface` | `#EFECE5` | `#201E1C` | panels, menus, dialogs, inputs | |
| `ink` | `#1F1D1A` | `#E8E4DC` | body text, headings | 15.4 / 14.3, 14.4 / 13.1 |
| `ink-muted` | `#5C574F` | `#ABA59A` | metadata, helper text | 6.6 / 6.1, 7.5 / 6.8 |
| `rule` | `#D6D1C7` | `#34312D` | decorative hairlines only | n/a (non-informational) |
| `control` | `#8A8478` | `#7A746A` | input and control borders | 3.4 / 3.2, 3.9 / 3.6 (3:1 for UI components) |
| `accent` | `#2B4C7E` | `#9DB8E6` | links, primary buttons, focus ring, current page | 7.9 / 7.3, 9.1 / 8.3 |
| `accent-ink` | `#FFFFFF` | `#12223A` | text on `accent` | 8.6, 7.9 |
| `success` | `#2F6B3A` | `#7CC48A` | accepted claims, saved | 5.9 / 5.4, 8.8 / 8.0 |
| `warning` | `#8A5A00` | `#E0B455` | pending, expiring drafts | 5.4 / 5.0, 9.4 / 8.6 |
| `error` | `#A3322A` | `#F08A80` | validation and request errors | 6.3 / 5.8, 7.5 / 6.9 |
| `info` | `#1F5F7A` | `#7FC4DD` | neutral notices | 6.5 / 6.0, 9.4 / 8.6 |

The accent is an ink blue, the colour of a fountain-pen annotation. Status
colours keep their meaning everywhere and MUST NOT be used as decoration or as a
second accent. Status is never carried by colour alone: it always has a word or
an icon beside it.

## Pagination (reading)

- A perspective of up to 10 parts renders **one part per page** as semantic
  HTML: an `<article>` with the perspective title as its heading and the part as
  paragraphs. No canvas, no page-flip, no curl, no slide.
- Each page has **previous** and **next** controls as real `<button>` or `<a>`
  elements, labelled ("Previous part", "Next part"), disabled at the ends, plus a
  "Part 3 of 7" position label announced to screen readers (`aria-live="polite"`).
- **Arrow keys page**: `ArrowLeft` goes back and `ArrowRight` goes forward while
  focus is not inside a text field. The part index is in the URL so a page is
  linkable and the browser back button returns to the previous part.
- After a page change focus moves to the part's heading so a screen reader
  starts reading the new part.

## Motion

- The only transition on a reading surface is a 150ms `opacity` cross-fade
  between parts. Interface transitions (menus, dialogs, tooltips) animate
  `opacity` and `transform` only, under 200ms, and never block input.
- Under `@media (prefers-reduced-motion: reduce)` every transition and
  animation duration is set to zero globally in `styles.css`; components MUST NOT
  override it.

## Interaction

- **Focus:** every focusable element shows a 2px `accent` outline with a 2px
  offset on `:focus-visible`. Outlines are never removed.
- **Targets:** buttons and links in navigation are at least 44px tall on touch
  layouts.
- **Hover** styling appears only on elements that respond to a click.
- **Selection** (current tab, current part, active filter) is shown by an
  underline or border in `accent` plus `aria-selected` or `aria-current`, never
  by opacity alone.
- **Keyboard:** every reading and writing action is reachable by Tab and
  operable by Enter or Space; dialogs trap focus and close on Escape.

## Components

The primitives live in `web/src/components/ui/` and wrap Radix so keyboard and
ARIA behaviour come from Radix, not from hand-written handlers:

| Component | Built on | Notes |
|---|---|---|
| `Button` | native `<button>` (Radix `Slot` for `asChild`) | variants `primary`, `secondary`, `ghost`, `danger`; sizes `sm`, `md` |
| `Input`, `Textarea` | native elements | `control` border, `surface` fill, `aria-invalid` turns the border `error` |
| `Dialog` | `@radix-ui/react-dialog` | `surface` panel, full width minus the gutter on phones |
| `Tabs` | `@radix-ui/react-tabs` | the epic's Ranked, Newest and Tales tabs |
| `Select` | `@radix-ui/react-select` | genre, report reason |
| `Tooltip` | `@radix-ui/react-tooltip` | supplementary only; never the sole label |
| `DropdownMenu` | `@radix-ui/react-dropdown-menu` | account menu, per-perspective actions |
| `Form` | `react-hook-form` with `@radix-ui/react-label` | `FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`; errors render inline under the field |

Icons are `lucide-react`, imported by name, at 16px in interface text and 20px
in navigation, with `aria-hidden` when a text label sits beside them.

## Landing (the opening page)

The landing page is the first page of a novel, not a product pitch. A
first-time reader learns what Perspectiv is from one paragraph of prose and is
asked one question. There is no feed (AC-006), no hero image, no feature grid
and no call-to-action banner.

- **Chapter mark.** The page opens with an asterism (`⁂`, `aria-hidden`) in
  `ink-muted`, centred over a short hairline, the way a chapter opens in print.
- **Title.** "Where were you?" in Literata 600 at `text-3xl`, rising to
  `text-5xl` from `sm`, left-aligned on the reading measure.
- **Opening paragraph.** The idea, told as prose at `text-prose` on the
  measure, with a drop cap: the first letter floats left across three lines at
  `--drop-cap` size in Literata 600 `ink`. The drop cap is decoration only; the
  paragraph reads the same without it, and assistive technology hears the
  plain text.
- **Second paragraph.** What a reader can do: read anything without an
  account, and write when they were there. `ink-muted`, no drop cap.
- **The question.** The place search is introduced by a label line in
  letter-spaced capitals ("Begin with a place", the `label-caps` utility:
  Public Sans 500, 13px, `ink-muted`) and drawn as a ruled line, not a box: a transparent field with a
  `control` bottom border that turns `accent` on focus, Literata at
  `text-prose`. Results keep their list form.
- **Section breaks.** "Your perspectives" (signed in) and "Browse by state"
  follow, each opened by the same asterism break instead of a heavy heading
  rule. Their headings use `label-caps`. States set as a
  multi-column list (one column on phones, three from `md`) so fifty names
  read as a page, not a wall of links.
- Everything stays on the reading measure and the 16px gutter; nothing on the
  page animates.

## Voice

Interface copy is plain and short: "Publish", "Claim character", "Vote to
remove". Book words appear where they are true (part, page, chapter of an arc)
and nowhere else.
