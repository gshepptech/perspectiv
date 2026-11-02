import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const sections = [
  "A place and a day",
  "Writing your story",
  "Characters and “that was me”",
  "How stories link into a lore",
  "How canon is chosen",
  "Voting a story out",
  "Hours",
  "What readers can do",
  "Your privacy",
];

async function openGuide() {
  const view = renderApp("/how-it-works", {});
  await screen.findByRole("heading", { level: 1, name: "How Perspectiv works" });
  return view;
}

describe("/how-it-works", () => {
  it("is a public page titled after its heading, with every section in order", async () => {
    await openGuide();
    expect(document.title).toBe("How Perspectiv works · Perspectiv");
    // Signed out: the shell offers sign-in, and the page still reads.
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
    const headings = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(sections);
    for (const name of sections) expect(screen.getByRole("region", { name })).toBeInTheDocument();
  });

  it("follows the front page's example, the evening at Moonbean Coffee", async () => {
    await openGuide();
    const example = screen.getByRole("figure", { name: /Example/ });
    expect(example).toHaveTextContent("Moonbean Coffee, Portland");
    expect(within(example).getByText("the guy in the yellow raincoat", { selector: "mark" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Characters and “that was me”" })).toHaveTextContent(
      "otter read wren's story and recognised the guy in the yellow raincoat.",
    );
  });

  it("states the rules the server enforces, with its numbers", async () => {
    await openGuide();
    const region = (name: string) => screen.getByRole("region", { name });
    expect(region("Writing your story")).toHaveTextContent("up to 10 parts of up to 300 words each");
    expect(region("Characters and “that was me”")).toHaveTextContent("up to 10 of them");
    expect(region("Characters and “that was me”")).toHaveTextContent("someone you turn down cannot claim that character again");
    expect(region("How stories link into a lore")).toHaveTextContent("within a week of being published stands alone as a tale");
    expect(region("How canon is chosen")).toHaveTextContent("never by readers");
    expect(region("How canon is chosen")).toHaveTextContent("The highest-ranked lore with 4 or more people is the canon of the day");
    expect(region("How canon is chosen")).toHaveTextContent("there are no downvotes");
    expect(region("Voting a story out")).toHaveTextContent("Once a lore has 4 or more people");
    expect(region("Voting a story out")).toHaveTextContent("never the story's own writer");
    expect(region("Voting a story out")).toHaveTextContent("more than half of the other members");
    expect(region("Voting a story out")).toHaveTextContent("any three of the other four");
    expect(region("Hours")).toHaveTextContent("Stories link only within the same day or the same hour");
    expect(region("Hours")).toHaveTextContent("under 21:00");
    expect(region("What readers can do")).toHaveTextContent(
      "Upvoting, claiming and posting in a lore's thread belong to the people who wrote about the day.",
    );
    expect(within(region("What readers can do")).getAllByRole("listitem")).toHaveLength(3);
  });

  it("says privacy in one line and links the privacy notice", async () => {
    await openGuide();
    const privacy = screen.getByRole("region", { name: "Your privacy" });
    expect(privacy.querySelectorAll("p")).toHaveLength(1);
    expect(privacy).toHaveTextContent("Other people see only your username, never your email address");
    expect(within(privacy).getByRole("link", { name: "privacy notice" })).toHaveAttribute("href", "/privacy");
  });

  it("calls a story a story, never a perspective", async () => {
    await openGuide();
    expect(screen.getByRole("main").textContent).not.toMatch(/\bperspectives?\b/i);
  });
});

describe("ways into the guide", () => {
  it("the footer links it from every page", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/terms", {});
    const footer = await screen.findByRole("navigation", { name: "About this site" });
    const link = within(footer).getByRole("link", { name: "How it works" });
    expect(link).toHaveAttribute("href", "/how-it-works");
    // It comes before the legal pages.
    expect(within(footer).getAllByRole("link")[0]).toBe(link);
    await user.click(link);
    expect(await screen.findByRole("heading", { level: 1, name: "How Perspectiv works" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/how-it-works");
  });
});
