import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useId, useRef } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { actionError } from "@/components/actions/errors";
import { Button } from "@/components/ui/Button";
import { Textarea } from "@/components/ui/Textarea";

const MAX_POST = 1000;

const schema = z.object({
  body: z
    .string()
    .refine((b) => b.trim() !== "", "Write something first.")
    .refine((b) => [...b.trim()].length <= MAX_POST, `Keep a post to ${MAX_POST} characters.`),
});
type Values = z.infer<typeof schema>;

type Props = {
  /** The field's visible label: "Add to the thread", "Reply to otter", "Edit your post". */
  label: string;
  submitLabel: string;
  pendingLabel: string;
  defaultBody?: string;
  /** Sends the trimmed text; calls `done` once the server took it. */
  send: (body: string, done: () => void) => void;
  pending: boolean;
  error: unknown;
  /** Present on a form that opened in place: Cancel and Escape close it. */
  onCancel?: () => void;
  rows?: number;
};

/**
 * The thread's one text form: a new post, a reply or an edit. An opened
 * form takes focus; the server's refusal shows under the field.
 */
export function PostForm({ label, submitLabel, pendingLabel, defaultBody = "", send, pending, error, onCancel, rows = 3 }: Props) {
  const fieldId = useId();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { body: defaultBody } });
  const invalid = form.formState.errors.body?.message;
  const field = form.register("body");
  const box = useRef<HTMLTextAreaElement | null>(null);
  // An in-place form takes focus once, when it opens.
  const inPlace = useRef(onCancel !== undefined);
  useEffect(() => {
    if (inPlace.current) box.current?.focus();
  }, []);

  return (
    <form noValidate className="grid gap-2" onSubmit={form.handleSubmit((v) => send(v.body.trim(), () => form.reset({ body: "" })))}>
      <label htmlFor={fieldId} className="text-sm font-medium text-ink">
        {label}
      </label>
      <Textarea
        id={fieldId}
        rows={rows}
        maxLength={MAX_POST}
        aria-invalid={invalid ? true : undefined}
        aria-describedby={invalid ? `${fieldId}-error` : undefined}
        {...field}
        ref={(el) => {
          field.ref(el);
          box.current = el;
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape" && onCancel) {
            e.preventDefault();
            onCancel();
          }
        }}
      />
      {invalid && (
        <p id={`${fieldId}-error`} role="alert" className="text-sm font-medium text-error">
          {invalid}
        </p>
      )}
      {error != null && (
        <p role="alert" className="text-sm text-error">
          {actionError(error)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" pending={pending}>
          {pending ? pendingLabel : submitLabel}
        </Button>
        {onCancel && (
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
