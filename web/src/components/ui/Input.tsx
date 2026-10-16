import type { ComponentProps } from "react";

import { cn } from "@/lib/cn";

export const fieldClasses =
  "w-full min-w-0 rounded-md border border-control bg-surface px-3 py-2 text-base text-ink placeholder:text-ink-muted " +
  "aria-invalid:border-error disabled:opacity-50";

export function Input({ className, type = "text", ...props }: ComponentProps<"input">) {
  return <input type={type} className={cn(fieldClasses, "min-h-11", className)} {...props} />;
}
