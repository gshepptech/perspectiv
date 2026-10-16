import { Slot } from "@radix-ui/react-slot";
import type { ComponentProps } from "react";

import { cn } from "@/lib/cn";

const variants = {
  primary: "bg-accent text-accent-ink hover:opacity-90",
  secondary: "border border-control bg-surface text-ink hover:border-ink",
  ghost: "text-ink hover:bg-surface",
  danger: "border border-error bg-paper text-error hover:bg-surface",
} as const;

const sizes = {
  sm: "min-h-9 px-3 text-sm",
  md: "min-h-11 px-4 text-base",
} as const;

export type ButtonProps = ComponentProps<"button"> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
  /** Render the single child element (a link, say) with button styling. */
  asChild?: boolean;
};

export function Button({ variant = "primary", size = "md", asChild = false, className, type, ...props }: ButtonProps) {
  const classes = cn(
    "inline-flex items-center justify-center gap-2 rounded-md font-sans font-medium transition-opacity duration-150",
    "disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50",
    variants[variant],
    sizes[size],
    className,
  );
  if (asChild) return <Slot className={classes} {...props} />;
  // A button outside a form must never submit by accident.
  return <button type={type ?? "button"} className={classes} {...props} />;
}
