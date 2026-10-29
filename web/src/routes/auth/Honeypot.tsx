import type { UseFormRegisterReturn } from "react-hook-form";

/**
 * A field people never see or reach: off screen, out of the tab order and
 * hidden from assistive technology. Automated form-fillers fill it, and the
 * server then answers as if the request worked and stores nothing.
 */
export function Honeypot({ field }: { field: UseFormRegisterReturn }) {
  return (
    <div aria-hidden className="pointer-events-none absolute -left-[10000px] h-px w-px overflow-hidden">
      <label>
        Website
        <input type="text" tabIndex={-1} autoComplete="off" {...field} />
      </label>
    </div>
  );
}
