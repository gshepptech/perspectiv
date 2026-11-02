import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const florida = { id: "st-fl", kind: "PLACE_KIND_STATE", name: "Florida" };
const tampa = { id: "ci-tpa", kind: "PLACE_KIND_CITY", name: "Tampa" };
const store = (id: string, name: string) => ({ id, kind: "PLACE_KIND_STORE", name, parentId: tampa.id, ancestors: [florida, tampa] });
const three = [store("pl-1", "Starbucks"), store("pl-2", "Starbucks Reserve"), store("pl-3", "Starbucks Airside")];

/** The front page with a place search answering from `answer`. */
function frontPage(answer: (query: string) => unknown[] = () => three, path = "/") {
  return renderApp(path, {
    "PlaceService/SearchPlaces": (body) => ({ body: { places: answer(String(body.query)) } }),
    "PlaceService/ListChildren": () => ({ body: { places: [] } }),
    "PlaceService/GetEpic": () => ({ body: { arcs: [] } }),
    "PlaceService/ListStoriedCategories": () => ({ body: { categories: [] } }),
  });
}

/** Types into the front page's field and waits for the three results. */
async function searched(user: ReturnType<typeof userEvent.setup>, text = "starbucks") {
  const field = await screen.findByRole("combobox", { name: "Search places" });
  await user.type(field, text);
  const list = await screen.findByRole("listbox", { name: "Matching places" });
  await within(list).findAllByRole("option");
  return { field, list };
}

/** The one option marked active, checked against the field's pointer to it. */
function activeOption(field: HTMLElement, list: HTMLElement): string | null {
  const selected = within(list)
    .getAllByRole("option")
    .filter((o) => o.getAttribute("aria-selected") === "true");
  expect(selected.length).toBeLessThanOrEqual(1);
  const active = selected[0];
  expect(field.getAttribute("aria-activedescendant")).toBe(active ? active.id : null);
  return active ? (active.textContent ?? "") : null;
}

describe("place search combobox (Folio, Place search)", () => {
  it("is a combobox that controls a listbox of options out of the tab order", async () => {
    const user = userEvent.setup();
    frontPage();
    const field = await screen.findByRole("combobox", { name: "Search places" });
    expect(field).toHaveAttribute("aria-expanded", "false");
    expect(field).toHaveAttribute("aria-autocomplete", "list");
    expect(field).not.toHaveAttribute("aria-activedescendant");
    const { list } = await searched(user);
    expect(field).toHaveAttribute("aria-expanded", "true");
    expect(field).toHaveAttribute("aria-controls", list.id);
    const options = within(list).getAllByRole("option");
    expect(options).toHaveLength(3);
    for (const o of options) {
      expect(o).toHaveAttribute("tabindex", "-1");
      expect(o).toHaveAttribute("aria-selected", "false");
    }
    // The whole row is the option's name, as a screen reader reads it.
    expect(options[0]).toHaveAccessibleName(/^Starbucks,\s*Store in Tampa, Florida · No stories yet$/);
  });

  it.each([
    { keys: "{ArrowDown}", want: "Starbucks" },
    { keys: "{ArrowDown}{ArrowDown}", want: "Starbucks Reserve" },
    { keys: "{ArrowUp}", want: "Starbucks Airside" },
    { keys: "{ArrowDown}{ArrowUp}", want: "Starbucks Airside" },
    { keys: "{ArrowUp}{ArrowDown}", want: "Starbucks" },
    { keys: "{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}", want: "Starbucks" },
  ])("$keys makes $want active, focus staying in the field", async ({ keys, want }) => {
    const user = userEvent.setup();
    frontPage();
    const { field, list } = await searched(user);
    const status = screen.getByRole("status").textContent;
    await user.keyboard(keys);
    expect(activeOption(field, list)).toMatch(new RegExp(`^${want},\\s*Store in`));
    expect(field).toHaveFocus();
    expect(field).toHaveValue("starbucks");
    // Moving through the list adds nothing to the status line.
    expect(screen.getByRole("status").textContent).toBe(status);
  });

  it.each([
    { name: "with none active opens the first result", keys: "", want: "/places/pl-1" },
    { name: "opens the active result", keys: "{ArrowDown}{ArrowDown}", want: "/places/pl-2" },
  ])("Enter $name", async ({ keys, want }) => {
    const user = userEvent.setup();
    const { router } = frontPage();
    await searched(user);
    await user.keyboard(`${keys}{Enter}`);
    await waitFor(() => expect(router.state.location.pathname).toBe(want));
  });

  it("Enter pressed before the answer for the text arrives opens its first result, not the earlier answer's", async () => {
    const user = userEvent.setup();
    const { router } = frontPage((q) => (q === "star" ? [store("pl-old", "Star Diner")] : three));
    const field = await screen.findByRole("combobox", { name: "Search places" });
    await user.type(field, "star");
    await screen.findByRole("option", { name: /Star Diner/ });
    // The earlier answer is still on screen when Enter lands.
    await user.type(field, "bucks{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/places/pl-1"));
  });

  it.each([
    { name: "when nothing matches", answer: () => [], text: "atlantis" },
    { name: "in a blank field", answer: () => three, text: "   " },
  ])("Enter does nothing $name", async ({ answer, text }) => {
    const user = userEvent.setup();
    const { router } = frontPage(answer);
    const field = await screen.findByRole("combobox", { name: "Search places" });
    await user.type(field, `${text}{Enter}`);
    if (text.trim()) await screen.findByText(`No places match “${text}”.`);
    await new Promise((r) => setTimeout(r, 300));
    expect(router.state.location.pathname).toBe("/");
    expect(field).toHaveValue(text);
  });

  it("Escape closes the list, keeping the text, then clears the field", async () => {
    const user = userEvent.setup();
    frontPage();
    const { field, list } = await searched(user);
    await user.keyboard("{ArrowDown}{Escape}");
    expect(field).toHaveAttribute("aria-expanded", "false");
    expect(field).not.toHaveAttribute("aria-activedescendant");
    expect(list).not.toBeVisible();
    expect(field).toHaveValue("starbucks");
    // An arrow reopens it.
    await user.keyboard("{ArrowDown}");
    expect(field).toHaveAttribute("aria-expanded", "true");
    expect(activeOption(field, list)).toMatch(/^Starbucks,\s*Store in/);
    await user.keyboard("{Escape}{Escape}");
    expect(field).toHaveValue("");
    expect(field).toHaveFocus();
  });

  it.each([
    { key: "{Home}", caret: 0 },
    { key: "{End}", caret: "starbucks".length },
  ])("$key moves the caret in the text and leaves no result active", async ({ key, caret }) => {
    const user = userEvent.setup();
    frontPage();
    const { field, list } = await searched(user);
    const input = field as HTMLInputElement;
    input.setSelectionRange(4, 4);
    await user.keyboard(`{ArrowDown}${key}`);
    expect(activeOption(field, list)).toBeNull();
    expect(field).toHaveAttribute("aria-expanded", "true");
    expect([input.selectionStart, input.selectionEnd]).toEqual([caret, caret]);
  });

  it("typing leaves no result active", async () => {
    const user = userEvent.setup();
    frontPage();
    const { field, list } = await searched(user);
    await user.keyboard("{ArrowDown}s");
    expect(activeOption(field, list)).toBeNull();
  });

  it("opens a result with Enter from the header dialog and closes the dialog", async () => {
    const user = userEvent.setup();
    const { router } = frontPage(() => three, "/terms");
    await user.click(await within(await screen.findByRole("navigation", { name: "Primary" })).findByRole("button", { name: "Search" }));
    const dialog = await screen.findByRole("dialog", { name: "Search places" });
    const field = within(dialog).getByRole("combobox", { name: "Search places" });
    await user.type(field, "starbucks");
    await within(dialog).findAllByRole("option");
    await user.keyboard("{ArrowUp}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/places/pl-3"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
