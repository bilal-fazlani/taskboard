// A ticket's entries on its page, grouped by type in reading order: open
// notes (with the box for leaving one), where it stands (the latest
// hand-off), decisions, reviews, learnings and proof. The page is for
// reading: the note box and each entry's Challenge are the only inputs.
import { useEffect, useId, useRef, useState } from "react";
import { X } from "lucide-react";
import { api, type DocumentMeta, type Entry } from "../api/client";
import EntryCard, { AgentName, EntryTime } from "./EntryCard";
import {
  entryExcerpt,
  findingsSummary,
  groupTicketEntries,
  openChallenges,
  reviewRound,
  type EntryThread,
} from "../lib/entries";
import { actionErrorMessage } from "../lib/saveError";
import type { OwnerEntries } from "../hooks/useOwnerEntries";

const SECTION_HEADING =
  "mb-2.5 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500";
const COUNT = "font-medium normal-case tracking-normal text-slate-600";
const EMPTY = "text-[13px] text-slate-600";

function Section({ title, count, children }: { title: string; count?: string; children: React.ReactNode }) {
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
function Stack({ children }: { children: React.ReactNode }) {
  return <div className="space-y-2">{children}</div>;
}

/** A fold for entries that are no longer the point of a section: handled notes, earlier hand-offs. */
function Fold({ label, children }: { label: string; children: React.ReactNode }) {
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

/**
 * The ticket's entry sections. `value` is the owner's entries (null until
 * loaded); `readOnly` (a deleted ticket) drops the note box and Challenge.
 */
export default function TicketEntries({
  ticketId,
  value,
  failed,
  documents,
  readOnly = false,
  onNoteLeft,
  onOpenDocument,
  now,
}: {
  ticketId: string;
  value: OwnerEntries | null;
  failed: boolean;
  documents: DocumentMeta[] | null;
  readOnly?: boolean;
  onNoteLeft?: () => void;
  onOpenDocument?: (doc: DocumentMeta) => void;
  now?: Date;
}) {
  const entries = value?.entries ?? [];
  const agents = value?.agents ?? {};
  const groups = groupTicketEntries(entries);
  const challenges = openChallenges(entries);
  const byId = new Map(entries.map((e) => [e.id, e]));
  const allReviews = groups.reviews.map((t) => t.entry);

  // The entry a new note challenges, set by an entry's Challenge.
  // Each Challenge counts up, so the box takes focus again even when the
  // same entry is challenged twice.
  const [about, setAbout] = useState<{ entry: Entry; seq: number } | null>(null);
  const startChallenge = (entry: Entry) => setAbout((prev) => ({ entry, seq: (prev?.seq ?? 0) + 1 }));
  // An entry's Challenge action; none on a read-only page.
  const challenge = readOnly ? undefined : startChallenge;

  const card = (t: EntryThread, extra: Partial<React.ComponentProps<typeof EntryCard>> = {}) => (
    <EntryCard
      key={t.entry.id}
      entry={t.entry}
      agents={agents}
      replaced={t.replaced}
      challenged={challenges.has(t.entry.id)}
      now={now}
      {...extra}
    />
  );

  const noteCard = (t: EntryThread, open: boolean) => {
    const target = t.entry.about ? byId.get(t.entry.about) : undefined;
    const handler = t.entry.handledBy ? agents[t.entry.handledBy] : undefined;
    return card(t, {
      tone: open ? "open-note" : "muted",
      challenged: false,
      head: t.entry.about ? (
        <p className="mb-1.5 border-l-2 border-slate-700 pl-2 text-[12.5px] text-slate-400">
          Challenges: {target ? `“${entryExcerpt(target.text)}”` : "an entry no longer shown"}
        </p>
      ) : undefined,
      meta: open ? (
        <span className="text-pink-400">open, agent sees it on its next call</span>
      ) : t.entry.handledAt ? (
        <span className="inline-flex flex-wrap items-center gap-1.5">
          handled by <AgentName agent={handler} />
          <EntryTime at={t.entry.handledAt} now={now} />
        </span>
      ) : undefined,
    });
  };

  const loading = value === null && !failed;
  const loadNote = failed ? (
    <p className={EMPTY}>Entries could not be loaded.</p>
  ) : loading ? (
    <p className={EMPTY}>Loading…</p>
  ) : null;

  const decisionsReplaced = groups.decisions.reduce((n, t) => n + t.replaced.length, 0);

  return (
    <>
      <Section title="Notes" count={groups.openNotes.length > 0 ? `${groups.openNotes.length} open` : undefined}>
        {/* The box still works when the read failed, so only the failure is said here. */}
        {failed && <p className={EMPTY}>Entries could not be loaded.</p>}
        {groups.openNotes.length > 0 && <Stack>{groups.openNotes.map((t) => noteCard(t, true))}</Stack>}
        {groups.handledNotes.length > 0 && (
          <Fold label={`${groups.handledNotes.length} handled`}>
            {groups.handledNotes.map((t) => noteCard(t, false))}
          </Fold>
        )}
        {!readOnly && (
          <NoteBox
            ticketId={ticketId}
            about={about?.entry ?? null}
            focusSeq={about?.seq ?? 0}
            onClearAbout={() => setAbout(null)}
            onLeft={() => {
              setAbout(null);
              onNoteLeft?.();
            }}
          />
        )}
      </Section>

      <Section title="Where it stands">
        {loadNote ??
          (groups.handOff ? (
            <>
              {card(groups.handOff, { tone: "handoff" })}
              {groups.earlierHandOffs.length > 0 && (
                <Fold
                  label={`${groups.earlierHandOffs.length} earlier hand-off${groups.earlierHandOffs.length === 1 ? "" : "s"}`}
                >
                  {groups.earlierHandOffs.map((t) => card(t))}
                </Fold>
              )}
            </>
          ) : (
            <p className={EMPTY}>No hand-off yet: an agent writes one when it stops, with where it stopped and the next step.</p>
          ))}
      </Section>

      <Section
        title="Decisions"
        count={
          groups.decisions.length > 0
            ? `${groups.decisions.length} current${decisionsReplaced > 0 ? ` · ${decisionsReplaced} replaced` : ""}`
            : undefined
        }
      >
        {loadNote ??
          (groups.decisions.length > 0 ? (
            <Stack>{groups.decisions.map((t) => card(t, { onChallenge: challenge }))}</Stack>
          ) : (
            <p className={EMPTY}>No decisions yet.</p>
          ))}
      </Section>

      <Section title="Reviews" count={groups.reviews.length > 0 ? String(groups.reviews.length) : undefined}>
        {loadNote ??
          (groups.reviews.length > 0 ? (
            <Stack>
              {groups.reviews.map((t) => {
                const round = reviewRound(t.entry, allReviews);
                const report = t.entry.reportDocument
                  ? documents?.find((d) => d.name === t.entry.reportDocument)
                  : undefined;
                const changes = t.entry.verdict === "changes";
                return card(t, {
                  typeLabel: `Review ${round}`,
                  head: (
                    <div className="mb-0.5 flex flex-wrap items-baseline gap-2.5">
                      <span
                        className={`font-mono text-[11px] font-semibold uppercase ${
                          changes ? "text-red-400" : "text-green-400"
                        }`}
                      >
                        {t.entry.verdict ?? ""}
                      </span>
                      <span className="text-xs text-slate-400">{findingsSummary(t.entry.findings)}</span>
                    </div>
                  ),
                  meta: t.entry.reportDocument ? (
                    report && onOpenDocument ? (
                      <button
                        type="button"
                        onClick={() => onOpenDocument(report)}
                        className="text-[12.5px] text-blue-400 hover:text-blue-300"
                      >
                        Full report: {t.entry.reportDocument}
                      </button>
                    ) : (
                      <span className="text-[12.5px]">Full report: {t.entry.reportDocument}</span>
                    )
                  ) : undefined,
                });
              })}
            </Stack>
          ) : (
            <p className={EMPTY}>No reviews yet.</p>
          ))}
      </Section>

      <Section title="Learnings" count={groups.learnings.length > 0 ? String(groups.learnings.length) : undefined}>
        {loadNote ??
          (groups.learnings.length > 0 ? (
            <Stack>{groups.learnings.map((t) => card(t, { onChallenge: challenge }))}</Stack>
          ) : (
            <p className={EMPTY}>No learnings yet.</p>
          ))}
      </Section>

      <Section title="Proof">
        {loadNote ??
          (groups.proofs.length > 0 ? (
            <Stack>{groups.proofs.map((t) => card(t, { onChallenge: challenge }))}</Stack>
          ) : (
            <p className={EMPTY}>Written when the ticket is finished: what was verified, how, and the result.</p>
          ))}
      </Section>

      {groups.other.length > 0 && (
        <Section title="Other entries" count={String(groups.other.length)}>
          <Stack>{groups.other.map((t) => card(t))}</Stack>
        </Section>
      )}
    </>
  );
}

/**
 * The box for leaving a note: the agent holding the ticket gets it on its
 * next call, or the next agent to start it. After an entry's Challenge it
 * points at that entry, which the line above the box says, with a way to
 * drop the pointer.
 */
function NoteBox({
  ticketId,
  about,
  focusSeq,
  onClearAbout,
  onLeft,
}: {
  ticketId: string;
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
      await api.entries.createNote({ ticketId }, { type: "note", text: trimmed, ...(about ? { about: about.id } : {}) });
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
          placeholder={
            about
              ? "Say what's wrong with it. The next agent replaces it or confirms it."
              : "Leave a note. The agent holding this ticket gets it on its next call, or the next agent to start it."
          }
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
