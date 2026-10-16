import type { ComponentProps } from "react";

import { cn } from "@/lib/cn";

import { fieldClasses } from "./Input";

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return <textarea className={cn(fieldClasses, "min-h-32 font-serif text-prose", className)} {...props} />;
}
