import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PenLine } from "lucide-react";
import { useId, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { actionError } from "@/components/actions/errors";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";

const MAX_NAME = 60;

const schema = z.object({
  name: z
    .string()
    .refine((n) => n.trim() !== "", "Give the lore a name.")
    .refine((n) => [...n.trim().replace(/\s+/g, " ")].length <= MAX_NAME, `Keep a name to ${MAX_NAME} characters.`),
});
type Values = z.infer<typeof schema>;

/**
 * Lets a member of a lore name it ("The Lot 7 tailgate"). The latest name
 * wins; the server checks membership again.
 */
export function NameLore({ loreId, current }: { loreId: string; current: string }) {
  const client = useQueryClient();
  const fieldId = useId();
  const [open, setOpen] = useState(false);
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { name: current } });
  const save = useMutation({
    mutationFn: (v: Values) => clients.lore.nameLore({ loreId, name: v.name }),
    onSuccess: async () => {
      setOpen(false);
      // The name titles the lore here and its block on the arc page.
      await Promise.all([
        client.invalidateQueries({ queryKey: ["lore", "getLore", { loreId }] }),
        client.invalidateQueries({ queryKey: ["arc"] }),
      ]);
    },
  });
  const error = form.formState.errors.name?.message;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          form.reset({ name: current });
          save.reset();
        }
        setOpen(next);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm">
          <PenLine aria-hidden size={16} />
          {current ? "Rename this lore" : "Name this lore"}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{current ? "Rename this lore" : "Name this lore"}</DialogTitle>
        <DialogDescription>
          Anyone in the lore can name it. Readers see the name as its title on the day's page.
        </DialogDescription>
        <form noValidate className="mt-4 grid gap-2" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
          <label htmlFor={fieldId} className="text-sm font-medium text-ink">
            Name
          </label>
          <Input
            id={fieldId}
            maxLength={MAX_NAME + 20}
            placeholder="The Lot 7 tailgate"
            aria-invalid={error ? true : undefined}
            {...form.register("name")}
          />
          {error && <p className="text-sm font-medium text-error">{error}</p>}
          {save.isError && (
            <p role="alert" className="text-sm text-error">
              {actionError(save.error)}
            </p>
          )}
          <Button type="submit" className="justify-self-start" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save name"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
