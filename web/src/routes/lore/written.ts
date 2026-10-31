import { useEffect, useRef, useState, type RefObject } from "react";

import type { LorePost } from "@/gen/perspectiv/v1/lore_pb";

/**
 * The server's copy of each post the viewer wrote into one list, shown at
 * once until the list's own read brings it (or for good, when it lands on a
 * page the reader has not loaded). An edit or a delete of one of them
 * updates the copy too, so a post the list has not read never shows stale
 * or comes back after it was deleted.
 */
export function useWritten() {
  const [written, setWritten] = useState<LorePost[]>([]);
  return {
    written,
    add: (post: LorePost) => setWritten((ws) => [...ws, post]),
    edited: (post: LorePost) => setWritten((ws) => ws.map((w) => (w.id === post.id ? { ...w, body: post.body, editedAt: post.editedAt } : w))),
    gone: (id: string) => setWritten((ws) => ws.filter((w) => w.id !== id)),
  };
}

/** `loaded`, then each written post `loaded` does not hold yet. */
export function withWritten(loaded: LorePost[], written: LorePost[]): LorePost[] {
  const ids = new Set(loaded.map((p) => p.id));
  return [...loaded, ...written.filter((w) => !ids.has(w.id))];
}

/**
 * Moves focus to a post inside `scope` once it has rendered: call the
 * returned function with its id. The post's article gets tabIndex -1 only
 * so it can take that focus.
 */
export function useFocusPost(scope: RefObject<HTMLElement | null>): (id: string) => void {
  const pending = useRef<string | null>(null);
  const [, setAsked] = useState(0);
  useEffect(() => {
    const id = pending.current;
    if (!id) return;
    const el = scope.current?.querySelector<HTMLElement>(`[data-post-id="${id}"]`);
    if (!el) return;
    pending.current = null;
    el.tabIndex = -1;
    el.focus();
  });
  return (id: string) => {
    pending.current = id;
    setAsked((n) => n + 1);
  };
}
