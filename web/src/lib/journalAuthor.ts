// The name the web UI appends journal entries as. There are no user accounts,
// so the person types it once beside Append and this browser remembers it.
//
// localStorage can be missing or throw (private windows, blocked site data),
// so every access is wrapped: storage that can't be used just means the name
// has to be typed again.

/** The localStorage key holding the journal author's name. */
export const JOURNAL_AUTHOR_KEY = "taskboard.journalAuthor";

/** The remembered author, or "" when there is none or storage can't be read. */
export function readJournalAuthor(): string {
  try {
    return globalThis.localStorage?.getItem(JOURNAL_AUTHOR_KEY) ?? "";
  } catch {
    return "";
  }
}

/** Remembers the author for the next entry. Storage that can't be written is ignored. */
export function rememberJournalAuthor(name: string): void {
  try {
    globalThis.localStorage?.setItem(JOURNAL_AUTHOR_KEY, name);
  } catch {
    // No storage, no memory.
  }
}
