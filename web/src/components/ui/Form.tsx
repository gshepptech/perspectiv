import * as LabelPrimitive from "@radix-ui/react-label";
import { Slot } from "@radix-ui/react-slot";
import { createContext, useContext, useId, type ComponentProps } from "react";
import {
  Controller,
  FormProvider,
  useFormContext,
  useFormState,
  type ControllerProps,
  type FieldPath,
  type FieldValues,
} from "react-hook-form";

import { cn } from "@/lib/cn";

/** Form is react-hook-form's provider: spread the useForm() result into it. */
export const Form = FormProvider;

const FieldContext = createContext<{ name: string } | null>(null);
const ItemContext = createContext<{ id: string } | null>(null);

export function FormField<TValues extends FieldValues, TName extends FieldPath<TValues>>(
  props: ControllerProps<TValues, TName>,
) {
  return (
    <FieldContext.Provider value={{ name: props.name }}>
      <Controller {...props} />
    </FieldContext.Provider>
  );
}

function useFormField() {
  const field = useContext(FieldContext);
  const item = useContext(ItemContext);
  if (!field || !item) throw new Error("form parts must sit inside <FormField> and <FormItem>");
  const { getFieldState } = useFormContext();
  const state = useFormState({ name: field.name });
  const fieldState = getFieldState(field.name, state);
  return {
    name: field.name,
    controlId: `${item.id}-control`,
    descriptionId: `${item.id}-description`,
    messageId: `${item.id}-message`,
    error: fieldState.error,
  };
}

export function FormItem({ className, ...props }: ComponentProps<"div">) {
  const id = useId();
  return (
    <ItemContext.Provider value={{ id }}>
      <div className={cn("grid gap-1.5", className)} {...props} />
    </ItemContext.Provider>
  );
}

export function FormLabel({ className, ...props }: ComponentProps<typeof LabelPrimitive.Root>) {
  const { controlId, error } = useFormField();
  return (
    <LabelPrimitive.Root
      htmlFor={controlId}
      data-invalid={error ? "" : undefined}
      className={cn("text-sm font-medium text-ink data-[invalid]:text-error", className)}
      {...props}
    />
  );
}

export function FormControl(props: ComponentProps<typeof Slot>) {
  const { controlId, descriptionId, messageId, error } = useFormField();
  return (
    <Slot
      id={controlId}
      aria-describedby={error ? `${descriptionId} ${messageId}` : descriptionId}
      aria-invalid={!!error}
      {...props}
    />
  );
}

export function FormDescription({ className, ...props }: ComponentProps<"p">) {
  const { descriptionId } = useFormField();
  return <p id={descriptionId} className={cn("text-sm text-ink-muted", className)} {...props} />;
}

/** The field's validation error, inline under the field; renders nothing when valid. */
export function FormMessage({ className, children, ...props }: ComponentProps<"p">) {
  const { messageId, error } = useFormField();
  const body = error?.message ? String(error.message) : children;
  if (!body) return null;
  return (
    <p id={messageId} role="alert" className={cn("text-sm font-medium text-error", className)} {...props}>
      {body}
    </p>
  );
}
