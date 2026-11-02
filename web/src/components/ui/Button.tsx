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

/**
 * A negative start margin equal to each size's padding, only while the button
 * is its row's first child: a ghost button that starts a row lines its icon
 * up with the text column, and one later in a row keeps its place.
 */
const flushes = {
  sm: "first:-ms-3",
  md: "first:-ms-4",
} as const;

export type ButtonProps = ComponentProps<"button"> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
  /** Render the single child element (a link, say) with button styling. */
  asChild?: boolean;
  /** Sits flush with the text column when it starts its row (Folio, Flush ghost buttons). */
  flush?: boolean;
  /**
   * A request is in flight: the button looks disabled and ignores presses,
   * but keeps keyboard focus. `disabled` would drop focus to the page, and a
   * keyboard or screen-reader user would lose their place.
   */
  pending?: boolean;
};

export function Button({
  variant = "primary",
  size = "md",
  asChild = false,
  flush = false,
  pending = false,
  className,
  type,
  onClick,
  ...props
}: ButtonProps) {
  const classes = cn(
    "inline-flex items-center justify-center gap-2 rounded-md font-sans font-medium transition-opacity duration-150",
    "disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50",
    variants[variant],
    sizes[size],
    flush && flushes[size],
    className,
  );
  if (asChild) return <Slot className={classes} onClick={onClick} {...props} />;
  return (
    <button
      // A button outside a form must never submit by accident.
      type={type ?? "button"}
      className={classes}
      aria-disabled={pending || undefined}
      aria-busy={pending || undefined}
      onClick={(e) => {
        // A pending press neither submits its form nor runs its handler.
        if (pending) {
          e.preventDefault();
          return;
        }
        onClick?.(e);
      }}
      {...props}
    />
  );
}
