// A ticket's entries on its page, grouped by type in reading order: open
// notes (with the box for leaving one), where it stands (the latest
// hand-off), decisions, reviews, learnings and proof. The page is for
// reading: the note box and each entry's Challenge are the only inputs.
import type { DocumentMeta } from "../api/client";
import EntryCard from "./EntryCard";
import { EntryFold, EntryHint, EntrySection, EntryStack, NoteBox, NoteCard } from "./EntryParts";
import {
  findingsSummary,
  groupTicketEntries,
  openChallenges,
  reviewRound,
  type EntryThread,
} from "../lib/entries";
import { useChallenge } from "../hooks/useChallenge";
import type { OwnerEntries } from "../hooks/useOwnerEntries";

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
  const { about, focusSeq, challenge, clear } = useChallenge(readOnly);

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

  const noteCard = (t: EntryThread, open: boolean) => (
    <NoteCard
      key={t.entry.id}
      thread={t}
      open={open}
      openLabel="open, agent sees it on its next call"
      agents={agents}
      byId={byId}
      now={now}
    />
  );

  const loading = value === null && !failed;
  const loadNote = failed ? (
    <EntryHint>Entries could not be loaded.</EntryHint>
  ) : loading ? (
    <EntryHint>Loading…</EntryHint>
  ) : null;

  const decisionsReplaced = groups.decisions.reduce((n, t) => n + t.replaced.length, 0);

  return (
    <>
      <EntrySection title="Notes" count={groups.openNotes.length > 0 ? `${groups.openNotes.length} open` : undefined}>
        {/* The box still works when the read failed, so only the failure is said here. */}
        {failed && <EntryHint>Entries could not be loaded.</EntryHint>}
        {groups.openNotes.length > 0 && <EntryStack>{groups.openNotes.map((t) => noteCard(t, true))}</EntryStack>}
        {groups.handledNotes.length > 0 && (
          <EntryFold label={`${groups.handledNotes.length} handled`}>
            {groups.handledNotes.map((t) => noteCard(t, false))}
          </EntryFold>
        )}
        {!readOnly && (
          <NoteBox
            owner={{ ticketId }}
            placeholder="Leave a note. The agent holding this ticket gets it on its next call, or the next agent to start it."
            about={about}
            focusSeq={focusSeq}
            onClearAbout={clear}
            onLeft={() => {
              clear();
              onNoteLeft?.();
            }}
          />
        )}
      </EntrySection>

      <EntrySection title="Where it stands">
        {loadNote ??
          (groups.handOff ? (
            <>
              {card(groups.handOff, { tone: "handoff" })}
              {groups.earlierHandOffs.length > 0 && (
                <EntryFold
                  label={`${groups.earlierHandOffs.length} earlier hand-off${groups.earlierHandOffs.length === 1 ? "" : "s"}`}
                >
                  {groups.earlierHandOffs.map((t) => card(t))}
                </EntryFold>
              )}
            </>
          ) : (
            <EntryHint>No hand-off yet: an agent writes one when it stops, with where it stopped and the next step.</EntryHint>
          ))}
      </EntrySection>

      <EntrySection
        title="Decisions"
        count={
          groups.decisions.length > 0
            ? `${groups.decisions.length} current${decisionsReplaced > 0 ? ` · ${decisionsReplaced} replaced` : ""}`
            : undefined
        }
      >
        {loadNote ??
          (groups.decisions.length > 0 ? (
            <EntryStack>{groups.decisions.map((t) => card(t, { onChallenge: challenge }))}</EntryStack>
          ) : (
            <EntryHint>No decisions yet.</EntryHint>
          ))}
      </EntrySection>

      <EntrySection title="Reviews" count={groups.reviews.length > 0 ? String(groups.reviews.length) : undefined}>
        {loadNote ??
          (groups.reviews.length > 0 ? (
            <EntryStack>
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
            </EntryStack>
          ) : (
            <EntryHint>No reviews yet.</EntryHint>
          ))}
      </EntrySection>

      <EntrySection title="Learnings" count={groups.learnings.length > 0 ? String(groups.learnings.length) : undefined}>
        {loadNote ??
          (groups.learnings.length > 0 ? (
            <EntryStack>{groups.learnings.map((t) => card(t, { onChallenge: challenge }))}</EntryStack>
          ) : (
            <EntryHint>No learnings yet.</EntryHint>
          ))}
      </EntrySection>

      <EntrySection title="Proof">
        {loadNote ??
          (groups.proofs.length > 0 ? (
            <EntryStack>{groups.proofs.map((t) => card(t, { onChallenge: challenge }))}</EntryStack>
          ) : (
            <EntryHint>Written when the ticket is finished: what was verified, how, and the result.</EntryHint>
          ))}
      </EntrySection>

      {groups.other.length > 0 && (
        <EntrySection title="Other entries" count={String(groups.other.length)}>
          <EntryStack>{groups.other.map((t) => card(t))}</EntryStack>
        </EntrySection>
      )}
    </>
  );
}
