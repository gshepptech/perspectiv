// The importer's rule for a name written all in lower case
// (internal/place/importer/names.go titleCase): each word capitalised,
// minor words left lower case unless they open or close the name. Kept in
// step with it so an added place reads like an imported one.

/** Words that stay lower case inside a title-cased name. */
const minorWords = new Set(["a", "an", "and", "at", "by", "for", "in", "of", "on", "or", "the", "to", "de", "del", "la"]);

/**
 * A word's first letter, and the first after each hyphen or slash, in upper
 * case; a letter after an apostrophe stays as it is ("joe's").
 */
function capitaliseParts(word: string): string {
  let out = "";
  let start = true;
  for (const ch of word) {
    const letter = /\p{L}/u.test(ch);
    if (start && letter) {
      out += ch.toUpperCase();
      start = false;
    } else {
      if (letter || /[\p{Nd}'’]/u.test(ch)) start = false;
      out += ch;
    }
    if (ch === "-" || ch === "/") start = true;
  }
  return out;
}

/** Title-cases an all-lower-case name word by word, as the importer does. */
export function titleCase(name: string): string {
  const words = name.split(/\s+/).filter(Boolean);
  return words.map((w, i) => (i > 0 && i < words.length - 1 && minorWords.has(w) ? w : capitaliseParts(w))).join(" ");
}

/**
 * The name to offer for a place someone typed: title case when they typed
 * it all in lower case, otherwise as they typed it, spaces collapsed.
 */
export function suggestPlaceName(typed: string): string {
  const name = typed.trim().split(/\s+/).filter(Boolean).join(" ");
  return name.toLowerCase() === name ? titleCase(name) : name;
}
