/**
 * The reader's Appearance choices (docs/design-system.md, Appearance). They
 * belong to the device: kept in localStorage, never sent to the server, and
 * applied as data attributes on the root element that styles.css reads.
 * public/appearance.js applies them before the first paint with the same
 * keys, values and colours.
 */
export type Theme = "system" | "light" | "dark";
export type TextSize = "standard" | "large" | "larger";

const THEME_KEY = "perspectiv.theme";
const TEXT_KEY = "perspectiv.text";

/** The page's theme-color for each theme, as index.html declares them. */
const THEME_COLOUR = { light: "#F7F5F0", dark: "#161514" } as const;

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // A blocked store keeps the choice for this page only.
  }
}

export function readTheme(): Theme {
  const v = read(THEME_KEY);
  return v === "light" || v === "dark" ? v : "system";
}

export function readTextSize(): TextSize {
  const v = read(TEXT_KEY);
  return v === "large" || v === "larger" ? v : "standard";
}

/** Sets the root's data-theme and the browser's theme-color for theme. */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") delete root.dataset.theme;
  else root.dataset.theme = theme;
  for (const meta of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    const forMedia = (meta.getAttribute("media") ?? "").includes("dark") ? THEME_COLOUR.dark : THEME_COLOUR.light;
    meta.content = theme === "system" ? forMedia : THEME_COLOUR[theme];
  }
}

function applyTextSize(size: TextSize): void {
  const root = document.documentElement;
  if (size === "standard") delete root.dataset.text;
  else root.dataset.text = size;
}

export function saveTheme(theme: Theme): void {
  write(THEME_KEY, theme === "system" ? null : theme);
  applyTheme(theme);
}

export function saveTextSize(size: TextSize): void {
  write(TEXT_KEY, size === "standard" ? null : size);
  applyTextSize(size);
}
