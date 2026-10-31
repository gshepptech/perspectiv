import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { applyTheme, readTextSize, readTheme } from "@/lib/appearance";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => {
  localStorage.clear();
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.text;
});

describe("appearance", () => {
  it("applies a theme and text size at once, keeps them on the device, and goes back to the device's theme", async () => {
    const user = userEvent.setup();
    renderApp("/appearance", {});
    expect(await screen.findByRole("heading", { level: 1, name: "Appearance" })).toHaveFocus();
    expect(document.title).toBe("Appearance · Perspectiv");
    expect(screen.getByRole("radio", { name: /Match this device/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Standard" })).toBeChecked();

    await user.click(screen.getByRole("radio", { name: "Lamplight (dark)" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(readTheme()).toBe("dark");
    await user.click(screen.getByRole("radio", { name: "Larger" }));
    expect(document.documentElement.dataset.text).toBe("larger");
    expect(readTextSize()).toBe("larger");
    expect(screen.getByRole("status")).toHaveTextContent("Saved on this device.");

    await user.click(screen.getByRole("radio", { name: /Match this device/ }));
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(localStorage.getItem("perspectiv.theme")).toBeNull();
  });

  it("opens with the saved choices and ignores values it does not know", async () => {
    localStorage.setItem("perspectiv.theme", "light");
    localStorage.setItem("perspectiv.text", "huge");
    renderApp("/appearance", {});
    expect(await screen.findByRole("radio", { name: "Paper (light)" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Standard" })).toBeChecked();
  });

  it("points the browser's theme colour at a forced theme and back at the device's", () => {
    document.head.insertAdjacentHTML(
      "beforeend",
      '<meta name="theme-color" content="#F7F5F0" media="(prefers-color-scheme: light)"><meta name="theme-color" content="#161514" media="(prefers-color-scheme: dark)">',
    );
    const metas = [...document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')];
    applyTheme("dark");
    expect(metas.map((m) => m.content)).toEqual(["#161514", "#161514"]);
    applyTheme("system");
    expect(metas.map((m) => m.content)).toEqual(["#F7F5F0", "#161514"]);
    for (const m of metas) m.remove();
  });

  it("is linked from the footer for every reader", async () => {
    renderApp("/terms", {});
    const footer = await screen.findByRole("navigation", { name: "About this site" });
    expect(footer.querySelector('a[href="/appearance"]')).toHaveTextContent("Appearance");
  });
});
