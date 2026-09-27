// The pieces every entry list is built from, on the ticket page and in the
// epic and project dialogs: a titled section, a stack of cards, a fold, a
// list that shows its newest few and pages the rest, a note's card, and the
// box for leaving a note or a challenge.
import { useEffect, useId, useRef, useState } from "react";
import { X } from "lucide-react";
import { api, type Entry, type EntryAgent, type EntryOwnerRef } from "../api/client";
import EntryCard, { AgentName, EntryTime } from "./EntryCard";
import { entryExcerpt, type EntryThread } from "../lib/entries";
import { actionErrorMessage } from "../lib/saveError";

const SECTION_HEADING =
  "mb-2.5 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500";
const COUNT = "font-medium normal-case tracking-normal text-slate-600";

/** A one-line hint in an empty or unloaded section. */
export function EntryHint({ children }: { children: React.ReactNode }) {
  return <p className="text-[13px] text-slate-600">{children}</p>;
}

/** A section of entries, named by its heading, with an optional count beside it. */
export function EntrySection({
  title,
  count,
  children,
}: {
  title: string;
  count?: string;
  children: React.ReactNode;
}) {
  const id = useId();
  return (
    <section aria-labelledby={id}>
      <h3 id={id} className={SECTION_HEADING}>
        {title}
        {count && <span className={COUNT}>{count}</span>}
      </h3>
      {children}
    </section>
  );
}

/** A section's cards, one under the other. */
export function EntryStack({ children }: { children: React.ReactNode }) {
  return <div className="space-y-2">{children}</div>;
}

/** A fold for entries that are no longer the point of a section: handled notes, earlier hand-offs. */
export function EntryFold({ label, children }: { label: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-2">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="text-[12.5px] text-slate-500 transition-colors hover:text-slate-300"
      >
        {open ? "▾" : "▸"} {label}
      </button>
      {open && <div className="mt-1.5 space-y-2">{children}</div>}
    </div>
  );
}

/** How many of a type show at first, and how many more each "Show N older" adds. */
export const FIRST_SHOWN = 3;
export const PAGE_SHOWN = 10;

/**
 * A type's threads, newest first: the FIRST_SHOWN newest, then "Show N older
 * <noun>", which adds PAGE_SHOWN more each time until all are shown. `noun`
 * is the plural ("decisions"); `one` the singular, for "Show 1 older decision".
 */
export function PagedEntries({
  threads,
  noun,
  one,
  render,
}: {
  threads: readonly EntryThread[];
  noun: string;
  one: string;
  render: (t: EntryThread) => React.ReactNode;
}) {
  const [shown, setShown] = useState(FIRST_SHOWN);
  const older = threads.length - shown;
  return (
    <>
      <EntryStack>{threads.slice(0, shown).map(render)}</EntryStack>
      {older > 0 && (
        <button
          type="button"
          onClick={() => setShown((n) => n + PAGE_SHOWN)}
          className="mt-2 text-[12.5px] text-blue-400 transition-colors hover:text-blue-300"
        >
          Show {older} older {older === 1 ? one : noun}
        </button>
      )}
    </>
  );
}

/**
 * A note as a card: what it challenges, when it points at an entry, and
 * whether it is open (`openLabel` says who gets it) or who handled it.
 */
export function NoteCard({
  thread,
  open,
  openLabel,
  agents,
  byId,
  now,
}: {
  thread: EntryThread;
  open: boolean;
  openLabel: string;
  agents: Record<string, EntryAgent>;
  byId: ReadonlyMap<string, Entry>;
  now?: Date;
}) {
  const { entry } = thread;
  const target = entry.about ? byId.get(entry.about) : undefined;
  const handler = entry.handledBy ? agents[entry.handledBy] : undefined;
  return (
    <EntryCard
      entry={entry}
      agents={agents}
      replaced={thread.replaced}
      now={now}
      tone={open ? "open-note" : "muted"}
      head={
        entry.about ? (
          <p className="mb-1.5 border-l-2 border-slate-700 pl-2 text-[12.5px] text-slate-400">
            Challenges: {target ? `“${entryExcerpt(target.text)}”` : "an entry no longer shown"}
          </p>
        ) : undefined
      }
      meta={
        open ? (
          <span className="text-pink-400">{openLabel}</span>
        ) : entry.handledAt ? (
          <span className="inline-flex flex-wrap items-center gap-1.5">
            handled by <AgentName agent={handler} />
            <EntryTime at={entry.handledAt} now={now} />
          </span>
        ) : undefined
      }
    />
  );
}

/**
 * The box for leaving a note on `owner`. After an entry's Challenge it
 * points at that entry, which the line above the box says, with a way to
 * drop the pointer.
 */
export function NoteBox({
  owner,
  placeholder,
  about,
  focusSeq,
  onClearAbout,
  onLeft,
}: {
  owner: EntryOwnerRef;
  /** What the empty box says: who gets the note. */
  placeholder: string;
  about: Entry | null;
  /** Counts Challenges: each new one brings the box into view and focuses it. */
  focusSeq: number;
  onClearAbout: () => void;
  onLeft: () => void;
}) {
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const aboutId = `${useId()}-about`;
  useEffect(() => {
    if (focusSeq === 0) return;
    const box = boxRef.current;
    box?.focus();
    // "nearest" scrolls only the content column, and only as far as needed:
    // a larger scroll can move the editor's own frame, header and all.
    box?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
  }, [focusSeq]);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = async () => {
    const trimmed = text.trim();
    if (!trimmed || sending) return;
    setSending(true);
    setError(null);
    try {
      await api.entries.createNote(owner, { type: "note", text: trimmed, ...(about ? { about: about.id } : {}) });
      setText("");
      onLeft();
    } catch (e) {
      setError(actionErrorMessage(e));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="mt-2">
      {about && (
        <div
          data-testid="note-about"
          className="mb-1.5 flex items-center gap-2 border-l-2 border-pink-400/50 pl-2 text-[12.5px] text-slate-400"
        >
          <span id={aboutId} className="min-w-0 flex-1 truncate">
            Challenges: “{entryExcerpt(about.text)}”
          </span>
          <button
            type="button"
            aria-label="Don't challenge this entry"
            title="Don't challenge this entry"
            onClick={onClearAbout}
            className="shrink-0 text-slate-500 hover:text-slate-300"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <textarea
          ref={boxRef}
          aria-label="Note"
          aria-describedby={about ? aboutId : undefined}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void send();
            }
          }}
          rows={2}
          placeholder={about ? "Say what's wrong with it. The next agent replaces it or confirms it." : placeholder}
          className="min-h-[2.75rem] flex-1 resize-y rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-[13px] text-slate-200 placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
        />
        <button
          type="submit"
          disabled={sending || !text.trim()}
          className="shrink-0 self-stretch rounded-lg bg-slate-700 px-3 text-[13px] font-medium text-white transition-colors hover:bg-slate-600 disabled:opacity-50"
        >
          Leave note
        </button>
      </form>
      {error && (
        <p role="alert" className="mt-1 text-xs text-red-300">
          {error}
        </p>
      )}
    </div>
  );
}
