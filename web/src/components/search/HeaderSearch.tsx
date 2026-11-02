import { Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";

import { PLACE_SEARCH_DEBOUNCE_MS, PlaceSearch, useDebounced } from "./PlaceSearch";

/** Input types that take typed text, where "/" is a character, not a shortcut. */
const nonTextInputs = new Set(["button", "checkbox", "color", "file", "image", "radio", "range", "reset", "submit"]);

/**
 * Whether "/" pressed now belongs to what has focus: a text field, text
 * area, select or editable text, or an open menu or dialog.
 */
function slashIsTaken(el: Element | null): boolean {
  if (document.querySelector('[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"]')) return true;
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable || el.closest('[role="menu"], [role="listbox"]')) return true;
  if (el instanceof HTMLInputElement) return !nonTextInputs.has(el.type);
  return el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement;
}

/** The dialog's field and results; unmounted on close, so it opens empty. */
function SearchBody({ onFollow }: { onFollow: () => void }) {
  const [text, setText] = useState("");
  const query = useDebounced(text.trim(), PLACE_SEARCH_DEBOUNCE_MS);
  return <PlaceSearch text={text} onTextChange={setText} query={query} onFollow={onFollow} className="mt-4" />;
}

/**
 * The header's Search item: a dialog with the front page's place search, on
 * every page. "/" opens it unless focus is where "/" is typed.
 */
export function HeaderSearch({ className }: { className: string }) {
  const [open, setOpen] = useState(false);
  // A followed result moves focus to its page's heading; closing must not
  // then pull focus back to the Search button.
  const followed = useRef(false);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
      if (slashIsTaken(document.activeElement)) return;
      e.preventDefault();
      setOpen(true);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button type="button" className={className} aria-keyshortcuts="/">
          <Search aria-hidden size={20} />
          <span>Search</span>
        </button>
      </DialogTrigger>
      <DialogContent
        // Pinned near the top so the field stays put as results arrive.
        className="top-4 translate-y-0 sm:top-[10vh] sm:max-h-[80dvh]"
        onCloseAutoFocus={(e) => {
          if (followed.current) e.preventDefault();
          followed.current = false;
        }}
      >
        <DialogTitle>Search places</DialogTitle>
        <DialogDescription>A place, or a place and its town: “starbucks tampa”.</DialogDescription>
        <SearchBody
          onFollow={() => {
            followed.current = true;
            setOpen(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
