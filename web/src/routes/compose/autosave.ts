import { useCallback, useEffect, useRef, useState } from "react";

/**
 * How long the composer waits after the last edit before it saves the draft.
 * 1.5 seconds: long enough that a typing burst becomes one SaveDraft call, short
 * enough that closing the tab right after a pause loses at most a sentence.
 * The composer also saves immediately before publishing and when it unmounts,
 * so the debounce never decides what gets published.
 */
export const AUTOSAVE_DEBOUNCE_MS = 1500;

export type AutosaveState = { kind: "idle" } | { kind: "pending" } | { kind: "saving" } | { kind: "saved" } | { kind: "failed"; message: string };

/**
 * Debounced, serialised saving. `schedule` restarts the debounce; when it
 * elapses `save` runs. Saves never overlap: the first save of a new draft is
 * what names it, so a second save started beside it would create a second
 * draft. Edits that arrive while a save is in flight queue exactly one more
 * save after it. `flush` skips the wait and resolves once nothing is pending
 * or in flight.
 */
export function useAutosave(save: () => Promise<void>, describeError: (err: unknown) => string) {
  const [state, setState] = useState<AutosaveState>({ kind: "idle" });
  const saveRef = useRef(save);
  const describeRef = useRef(describeError);
  saveRef.current = save;
  describeRef.current = describeError;

  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inflight = useRef<Promise<void> | null>(null);
  const dirty = useRef(false);
  const mounted = useRef(true);

  const set = useCallback((next: AutosaveState) => {
    if (mounted.current) setState(next);
  }, []);

  const run = useCallback((): Promise<void> => {
    if (inflight.current) return inflight.current;
    if (!dirty.current) return Promise.resolve();
    dirty.current = false;
    set({ kind: "saving" });
    const p = (async () => {
      // Yield once so inflight.current below is assigned before this body ends.
      await Promise.resolve();
      try {
        await saveRef.current();
        if (!dirty.current) set({ kind: "saved" });
      } catch (err) {
        set({ kind: "failed", message: describeRef.current(err) });
      }
      inflight.current = null;
      // Edits made during this save are not in it. A waiting debounce timer
      // saves them itself; without one (the timer fired mid-save), save now.
      if (dirty.current && !timer.current) await run();
    })();
    inflight.current = p;
    return p;
  }, [set]);

  const schedule = useCallback(() => {
    dirty.current = true;
    set({ kind: "pending" });
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      timer.current = null;
      void run();
    }, AUTOSAVE_DEBOUNCE_MS);
  }, [run, set]);

  const flush = useCallback(async (): Promise<void> => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    // Wait out a save in flight, then save whatever arrived during it.
    while (inflight.current || dirty.current) await run();
  }, [run]);

  /** Forgets unsaved edits without saving them (after a publish, say). */
  const cancel = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    dirty.current = false;
  }, []);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      // Leaving the composer saves what is pending rather than dropping it.
      // Failures are already turned into state by run(), so nothing escapes.
      if (timer.current) {
        clearTimeout(timer.current);
        timer.current = null;
        void run();
      }
    };
  }, [run]);

  return { state, setState: set, schedule, flush, cancel };
}
