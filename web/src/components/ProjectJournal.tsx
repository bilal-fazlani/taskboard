import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { BookText, ChevronDown, Lock } from "lucide-react";
import Markdown from "react-markdown";
import { api, type JournalEntry, type JournalPage } from "../api/client";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { activityTime } from "../lib/activity";
import { readJournalAuthor, rememberJournalAuthor } from "../lib/journalAuthor";

// A project's journal on the Edit Project form: a composer to append an entry,
// then the entries, newest first, a page at a time. Entries are never edited
// or deleted, so there is nothing to do to one once it is written. Appending
// is its own write, straight to the server, apart from the form's Save.

type Loaded = "loading" | "ready" | "error";

/** Entries in `incoming` that `known` doesn't have yet. */
function unseen(known: readonly JournalEntry[], incoming: readonly JournalEntry[]): JournalEntry[] {
  const ids = new Set(known.map((e) => e.id));
  return incoming.filter((e) => !ids.has(e.id));
}

export default function ProjectJournal({ projectId }: { projectId: string }) {
  const [entries, setEntries] = useState<JournalEntry[]>([]);
  const [total, setTotal] = useState(0);
  // Where the next older page starts, or null when the oldest entry is shown.
  const [nextBefore, setNextBefore] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Loaded>("loading");
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [olderError, setOlderError] = useState(false);
  const [text, setText] = useState("");
  const [author, setAuthor] = useState(readJournalAuthor);
  const [appending, setAppending] = useState(false);
  const [appendError, setAppendError] = useState("");
  const ids = useId();

  // Once an older page has been asked for, a newer first page (a live
  // refresh) only adds the entries written since, keeping the ones shown and
  // the cursor to the next older page where they are: replacing them while
  // that page loads would move the cursor and lose the entries between.
  // Until then a first page simply replaces what is shown.
  const olderShown = useRef(false);
  // The ids shown, for telling whether an appended entry is already there.
  const shownIds = useRef(new Set<string>());
  useEffect(() => {
    shownIds.current = new Set(entries.map((e) => e.id));
  }, [entries]);
  const textRef = useRef<HTMLTextAreaElement>(null);

  const showFirstPage = useCallback((page: JournalPage) => {
    if (olderShown.current) {
      setEntries((shown) => [...unseen(shown, page.entries), ...shown]);
    } else {
      setEntries(page.entries);
      setNextBefore(page.hasMore ? (page.nextBefore ?? null) : null);
    }
    setTotal(page.total);
    setLoaded("ready");
  }, []);

  useEffect(() => {
    let cancelled = false;
    api.projects.journal(projectId).then(
      (page) => {
        if (!cancelled) showFirstPage(page);
      },
      () => {
        if (!cancelled) setLoaded("error");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [projectId, showFirstPage]);

  // Entries an agent appends while the form is open appear at the top. A
  // failed refresh keeps what is shown.
  const refresh = useCallback(() => {
    api.projects.journal(projectId).then(showFirstPage, () => {});
  }, [projectId, showFirstPage]);
  useLiveRefresh(refresh);

  const showOlder = async () => {
    if (!nextBefore || loadingOlder) return;
    setLoadingOlder(true);
    setOlderError(false);
    olderShown.current = true;
    try {
      const page = await api.projects.journal(projectId, { before: nextBefore });
      setEntries((shown) => [...shown, ...unseen(shown, page.entries)]);
      setNextBefore(page.hasMore ? (page.nextBefore ?? null) : null);
      setTotal(page.total);
    } catch {
      setOlderError(true);
    }
    setLoadingOlder(false);
  };

  const canAppend = text.trim() !== "" && author.trim() !== "" && !appending;

  const append = async () => {
    if (!canAppend) return;
    setAppending(true);
    setAppendError("");
    try {
      const entry = await api.projects.appendJournal(projectId, { author: author.trim(), text });
      rememberJournalAuthor(author.trim());
      setText("");
      // A live refresh may have brought the entry, and its total, already.
      if (!shownIds.current.has(entry.id)) {
        setEntries((shown) => (shown.some((e) => e.id === entry.id) ? shown : [entry, ...shown]));
        setTotal((n) => n + 1);
      }
    } catch (err) {
      setAppendError(err instanceof Error ? err.message : "Could not append the entry.");
    }
    setAppending(false);
    // The Append button is disabled while it works, which takes the focus
    // away; give it back to the text for the next entry.
    textRef.current?.focus();
  };

  // Enter in the name field, or Cmd/Ctrl+Enter in the text, appends.
  const onTextKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      append();
    }
  };
  const onAuthorKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      append();
    }
  };

  return (
    <section aria-labelledby={`${ids}-heading`} className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2">
        <BookText aria-hidden="true" className="h-4 w-4 text-slate-400" />
        <h3 id={`${ids}-heading`} className="text-sm font-semibold text-white">
          Journal
        </h3>
        {loaded === "ready" && (
          <span
            data-testid="journal-total"
            title={`${total} ${total === 1 ? "entry" : "entries"}`}
            className="rounded-full bg-slate-800 px-2 py-0.5 text-[11px] text-slate-400"
          >
            {total}
          </span>
        )}
      </div>
      <p className="mt-1 flex items-center gap-1 text-xs text-slate-500">
        <Lock aria-hidden="true" className="h-3 w-3 shrink-0" />
        Dated notes, newest first. Entries are never edited or deleted.
      </p>

      <div className="mt-3 shrink-0 rounded-lg border border-slate-700 bg-slate-800/60 focus-within:ring-1 focus-within:ring-blue-500">
        <textarea
          ref={textRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onTextKeyDown}
          rows={3}
          aria-label="New journal entry"
          placeholder="Add to the journal… (Markdown)"
          className="block w-full resize-y rounded-t-lg bg-transparent px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none"
        />
        <div className="flex items-center justify-between gap-2 border-t border-slate-700/70 px-2 py-1.5">
          <label className="flex min-w-0 items-center gap-1.5 text-xs text-slate-500">
            as
            <input
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              onKeyDown={onAuthorKeyDown}
              aria-label="Author"
              placeholder="Your name"
              maxLength={100}
              className="h-7 w-32 min-w-0 rounded-md border border-slate-700 bg-slate-900 px-2 text-xs text-slate-200 placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </label>
          <button
            type="button"
            onClick={append}
            disabled={!canAppend}
            className="h-7 shrink-0 rounded-md bg-blue-600 px-3 text-xs font-medium text-white transition-colors hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-400"
          >
            {appending ? "Appending…" : "Append"}
          </button>
        </div>
      </div>
      {appendError && (
        <p role="alert" className="mt-1.5 text-xs text-red-400">
          {appendError}
        </p>
      )}

      <div className="mt-4 lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:pr-1">
        {loaded === "loading" && <p className="text-xs text-slate-600">Loading journal…</p>}
        {loaded === "error" && <p className="text-xs text-red-400">The journal could not be loaded.</p>}
        {loaded === "ready" && entries.length === 0 && (
          <p className="text-xs text-slate-600">No journal entries yet.</p>
        )}
        {entries.length > 0 && (
          <ol className="space-y-4">
            {entries.map((entry) => (
              <li key={entry.id} data-testid="journal-entry" className="border-l-2 border-slate-700 pl-3">
                <div className="flex items-baseline justify-between gap-3">
                  <span className="min-w-0 truncate text-xs font-medium text-slate-200">{entry.author}</span>
                  <time
                    dateTime={entry.createdAt}
                    title={new Date(entry.createdAt).toLocaleString()}
                    className="shrink-0 whitespace-nowrap text-[11px] text-slate-500"
                  >
                    {activityTime(entry.createdAt)}
                  </time>
                </div>
                <div className="prose-card mt-1 break-words text-sm text-slate-300">
                  <Markdown>{entry.text}</Markdown>
                </div>
              </li>
            ))}
          </ol>
        )}
        {nextBefore && (
          <button
            type="button"
            onClick={showOlder}
            disabled={loadingOlder}
            className="mt-4 inline-flex items-center gap-1 text-xs text-slate-400 transition-colors hover:text-slate-200 disabled:opacity-60"
          >
            {loadingOlder ? "Loading…" : "Show older entries"}
            <ChevronDown aria-hidden="true" className="h-3 w-3" />
          </button>
        )}
        {olderError && <p className="mt-1.5 text-xs text-red-400">Older entries could not be loaded.</p>}
      </div>
    </section>
  );
}
