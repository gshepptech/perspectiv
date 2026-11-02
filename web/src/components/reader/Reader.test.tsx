import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Reader, SHORT_STORY_WORDS, wordCount } from "./Reader";

const parts = [
  "The doors opened at nine.\n\nA man in a green coat ran for the escalator.",
  "I followed him past the fountain.",
  "He stopped at the bookshop and bought nothing.",
];

function Harness({ initial = 1, onChange }: { initial?: number; onChange?: (n: number) => void }) {
  const [part, setPart] = useState(initial);
  return (
    <>
      <label>
        Note
        <input />
      </label>
      <Reader
        title="Saturday at Riverside Mall"
        parts={parts}
        part={part}
        onPartChange={(n) => {
          onChange?.(n);
          setPart(n);
        }}
      />
    </>
  );
}

/** Installs a matchMedia that reports reduced motion as `reduced`. */
function stubMotion(reduced: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: reduced && query.includes("prefers-reduced-motion: reduce"),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
}

/** jsdom has no Web Animations API; record what the reader asks for. */
function stubAnimate() {
  const animate = vi.fn(() => ({ cancel: vi.fn(), finished: Promise.resolve() }));
  Object.defineProperty(HTMLElement.prototype, "animate", { value: animate, configurable: true, writable: true });
  return animate;
}

afterEach(() => {
  vi.unstubAllGlobals();
  delete (HTMLElement.prototype as { animate?: unknown }).animate;
});

describe("Reader pages one part at a time (OT-033)", () => {
  it("renders only the current part, as paragraphs inside an article headed by the title", () => {
    render(<Harness />);
    const article = screen.getByRole("article");
    expect(screen.getByRole("heading", { level: 1, name: "Saturday at Riverside Mall" })).toBeInTheDocument();
    const paragraphs = article.querySelectorAll("p");
    expect([...paragraphs].map((p) => p.textContent)).toEqual([
      "The doors opened at nine.",
      "A man in a green coat ran for the escalator.",
    ]);
    expect(screen.queryByText("I followed him past the fountain.")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Part 1" })).toBeInTheDocument();
  });

  it("keeps single line breaks inside a paragraph", () => {
    render(<Reader title="T" parts={["one line\nnext line"]} part={1} onPartChange={() => {}} />);
    const p = screen.getByRole("article").querySelector("p");
    expect(p?.innerHTML).toBe("one line<br>next line");
  });

  it("has no canvas element", () => {
    const { container } = render(<Harness />);
    expect(container.querySelector("canvas")).toBeNull();
  });

  it("announces the position politely", () => {
    render(<Harness initial={2} />);
    const status = screen.getByText("Part 2 of 3");
    expect(status).toHaveAttribute("aria-live", "polite");
  });

  const positions = [
    { name: "announces a position its caller names", announce: true, live: "polite" },
    { name: "leaves a position its caller announces itself silent", announce: false, live: null },
  ];
  for (const c of positions) {
    it(c.name, () => {
      render(<Reader title="Chapter 2: otter" parts={parts} part={2} onPartChange={() => {}} position="Chapter 2, part 2 of 3" announcePosition={c.announce} />);
      expect(screen.getByText("Chapter 2, part 2 of 3").getAttribute("aria-live")).toBe(c.live);
    });
  }

  it("turns one part per key even when keys come faster than the page redraws", () => {
    // The parent has not redrawn yet: part stays 1 while two keys arrive.
    const onPartChange = vi.fn();
    render(<Reader title="A story" parts={parts} part={1} onPartChange={onPartChange} />);
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    // A third key past the last part does nothing.
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(onPartChange.mock.calls).toEqual([[2], [3]]);
  });

  it("leaves the arrow keys to a caller that turns pages itself", async () => {
    const onPartChange = vi.fn();
    render(<Reader title="Chapter 1: wren" parts={parts} part={2} onPartChange={onPartChange} arrowKeys={false} />);
    await userEvent.keyboard("{ArrowRight}{ArrowLeft}");
    expect(onPartChange).not.toHaveBeenCalled();
    // The buttons still turn the part.
    await userEvent.click(screen.getByRole("button", { name: "Next part" }));
    expect(onPartChange).toHaveBeenCalledWith(3);
  });

  it("pages with labelled previous and next buttons, disabled at the ends", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    const prev = screen.getByRole("button", { name: "Previous part" });
    const next = screen.getByRole("button", { name: "Next part" });
    expect(prev).toBeDisabled();
    expect(next).toBeEnabled();
    await user.click(next);
    expect(screen.getByText("I followed him past the fountain.")).toBeInTheDocument();
    expect(prev).toBeEnabled();
    await user.click(next);
    expect(screen.getByText("He stopped at the bookshop and bought nothing.")).toBeInTheDocument();
    expect(next).toBeDisabled();
    await user.click(prev);
    expect(screen.getByText("Part 2 of 3")).toBeInTheDocument();
  });

  it("operates from the keyboard with Tab and Enter", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    screen.getByRole("button", { name: "Next part" }).focus();
    await user.keyboard("{Enter}");
    expect(screen.getByText("Part 2 of 3")).toBeInTheDocument();
  });

  it("pages with the arrow keys", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    await user.keyboard("{ArrowRight}");
    expect(screen.getByText("Part 2 of 3")).toBeInTheDocument();
    await user.keyboard("{ArrowRight}");
    await user.keyboard("{ArrowRight}");
    expect(screen.getByText("Part 3 of 3")).toBeInTheDocument();
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByText("Part 2 of 3")).toBeInTheDocument();
    // The third ArrowRight at the last part changed nothing.
    expect(onChange.mock.calls.map(([n]) => n)).toEqual([2, 3, 2]);
  });

  it("does not page while focus is in a text field or a modifier is held", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(screen.getByRole("textbox", { name: "Note" }));
    await user.keyboard("{ArrowRight}");
    expect(screen.getByText("Part 1 of 3")).toBeInTheDocument();
    screen.getByRole("textbox").blur();
    fireEvent.keyDown(document.body, { key: "ArrowRight", altKey: true });
    fireEvent.keyDown(document.body, { key: "ArrowRight", metaKey: true });
    expect(screen.getByText("Part 1 of 3")).toBeInTheDocument();
  });

  it("moves focus to the new part's heading after a page change, not on first render", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    expect(document.body).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("heading", { level: 2, name: "Part 2" })).toHaveFocus();
  });
});

describe("Reader motion (OT-034)", () => {
  it("cross-fades a page change with opacity only, no flip or slide", async () => {
    stubMotion(false);
    const animate = stubAnimate();
    render(<Harness />);
    expect(animate).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.keyDown(document.body, { key: "ArrowRight" });
    });
    expect(animate).toHaveBeenCalledOnce();
    const [keyframes, options] = animate.mock.calls[0] as unknown as [Keyframe[], KeyframeAnimationOptions];
    for (const frame of keyframes) expect(Object.keys(frame).filter((k) => k !== "offset" && k !== "easing")).toEqual(["opacity"]);
    expect(options.duration).toBeLessThanOrEqual(150);
  });

  it("changes pages with no transition when prefers-reduced-motion is set", async () => {
    stubMotion(true);
    const animate = stubAnimate();
    render(<Harness />);
    await act(async () => {
      fireEvent.keyDown(document.body, { key: "ArrowRight" });
    });
    expect(screen.getByText("I followed him past the fountain.")).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
    expect(screen.getByRole("article").className).not.toMatch(/transition|animate/);
  });
});

describe("Reader with every part on one page", () => {
  it("shows every part under its own heading, with no paging and inert arrow keys", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Reader title="T" parts={parts} part={1} onPartChange={onChange} whole />);
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(["Part 1", "Part 2", "Part 3"]);
    expect(screen.getByText("He stopped at the bookshop and bought nothing.")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull();
    await user.keyboard("{ArrowRight}");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("leaves the part heading off a one-part story", () => {
    render(<Reader title="T" parts={["Only this."]} part={1} onPartChange={() => {}} whole />);
    expect(screen.queryByRole("heading", { level: 2 })).toBeNull();
    expect(screen.getByText("Only this.")).toBeInTheDocument();
  });

  it("offers the toggle only when asked to, as a pressed or unpressed button", async () => {
    const user = userEvent.setup();
    const onWhole = vi.fn();
    const { rerender } = render(<Reader title="T" parts={parts} part={1} onPartChange={() => {}} />);
    expect(screen.queryByRole("button", { name: "Read all parts on one page" })).toBeNull();
    rerender(<Reader title="T" parts={parts} part={1} onPartChange={() => {}} onWholeChange={onWhole} />);
    const toggle = screen.getByRole("button", { name: "Read all parts on one page" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    await user.click(toggle);
    expect(onWhole).toHaveBeenCalledWith(true);
  });

  it("counts words across parts against the short-story limit", () => {
    expect(wordCount(["one two", "three\n\nfour  five"])).toBe(5);
    expect(SHORT_STORY_WORDS).toBe(600);
  });
});
