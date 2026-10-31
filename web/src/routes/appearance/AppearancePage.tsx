import { useEffect, useRef, useState } from "react";

import { readTextSize, readTheme, saveTextSize, saveTheme, type TextSize, type Theme } from "@/lib/appearance";
import { useDocumentTitle } from "@/lib/title";

type Option<T extends string> = { value: T; label: string; hint?: string };

const themes: readonly Option<Theme>[] = [
  { value: "system", label: "Match this device", hint: "Light or dark as your device is set." },
  { value: "light", label: "Paper (light)" },
  { value: "dark", label: "Lamplight (dark)" },
];

const sizes: readonly Option<TextSize>[] = [
  { value: "standard", label: "Standard" },
  { value: "large", label: "Large" },
  { value: "larger", label: "Larger" },
];

function Choice<T extends string>({
  name,
  legend,
  options,
  value,
  onChange,
}: {
  name: string;
  legend: string;
  options: readonly Option<T>[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <fieldset className="grid min-w-0 gap-1">
      <legend className="text-xl font-serif font-semibold">{legend}</legend>
      {options.map((o) => (
        <label key={o.value} className="flex min-h-11 cursor-pointer items-center gap-3 py-1 text-base">
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            onChange={() => onChange(o.value)}
            className="size-5 shrink-0 accent-accent"
          />
          <span className="min-w-0">
            {o.label}
            {o.hint && <span className="block text-sm text-ink-muted">{o.hint}</span>}
          </span>
        </label>
      ))}
    </fieldset>
  );
}

/**
 * /appearance: the theme and the story text size for this device, open to
 * every reader. A choice applies at once; nothing is sent to the server.
 */
export function Component() {
  useDocumentTitle("Appearance");
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [size, setSize] = useState<TextSize>(readTextSize);
  const [saved, setSaved] = useState(false);
  return (
    <div className="mx-auto grid w-full max-w-2xl min-w-0 gap-8">
      <h1 ref={heading} tabIndex={-1} className="text-2xl focus:outline-none sm:text-3xl">
        Appearance
      </h1>
      <Choice
        name="theme"
        legend="Theme"
        options={themes}
        value={theme}
        onChange={(v) => {
          setTheme(v);
          saveTheme(v);
          setSaved(true);
        }}
      />
      <div className="grid gap-3">
        <Choice
          name="text-size"
          legend="Story text size"
          options={sizes}
          value={size}
          onChange={(v) => {
            setSize(v);
            saveTextSize(v);
            setSaved(true);
          }}
        />
        <p className="prose-body text-ink-muted">The doors opened at nine, and the line already wrapped past the fountain.</p>
      </div>
      <p role="status" className="text-sm text-ink-muted">
        {saved ? "Saved on this device." : "These choices are kept on this device only."}
      </p>
    </div>
  );
}
