// An epic's or a project's entries, read once here rather than on every
// ticket: open notes with the box for leaving one, then decisions and
// learnings, each the newest few with the rest a page at a time. The same
// cards as the ticket page, so replaced entries sit under the entry that
// replaced them, and the note box and each entry's Challenge are the only
// inputs. Loaded on open and again on every live change.
import { useMemo, useRef, type RefObject } from "react";
import EntryCard from "./EntryCard";
import { EntryFold, EntryHint, EntrySection, EntryStack, NoteBox, NoteCard, PagedEntries } from "./EntryParts";
import { groupTicketEntries, openChallenges, type EntryThread } from "../lib/entries";
import { useChallenge } from "../hooks/useChallenge";
import { useOwnerEntries } from "../hooks/useOwnerEntries";
import { useScrollToEntries } from "../hooks/useScrollToEntries";

const PLACEHOLDER = {
  epic: "Leave a note for agents working in this epic. Every ticket's one-call start carries it until an agent handles it.",
  project: "Leave a note for any agent working in this project.",
};

/**
 * `scrollParent` is the dialog's scrolling box: opened from a link ending in
 * #entries, it scrolls to these entries once they have loaded, and focus
 * moves to them.
 */
export default function LevelEntries({
  owner,
  scrollParent,
  readOnly = false,
  now,
}: {
  owner: { epicId: string } | { projectId: string };
  scrollParent?: RefObject<HTMLElement | null>;
  readOnly?: boolean;
  now?: Date;
}) {
  const noun = "epicId" in owner ? "epic" : "project";
  const id = "epicId" in owner ? owner.epicId : owner.projectId;
  // The owner as the reads and the note box take it.
  const ref = useMemo(() => (noun === "epic" ? { epicId: id } : { projectId: id }), [noun, id]);
  const { value, failed, reload } = useOwnerEntries(ref);
  const { about, focusSeq, challenge, clear } = useChallenge(readOnly);
  const rootRef = useRef<HTMLDivElement>(null);
  useScrollToEntries(scrollParent, rootRef, value !== null || failed);

  const entries = value?.entries ?? [];
  const agents = value?.agents ?? {};
  const groups = groupTicketEntries(entries);
  const challenges = openChallenges(entries);
  const byId = new Map(entries.map((e) => [e.id, e]));

  const card = (t: EntryThread, canChallenge: boolean) => (
    <EntryCard
      key={t.entry.id}
      entry={t.entry}
      agents={agents}
      replaced={t.replaced}
      challenged={challenges.has(t.entry.id)}
      onChallenge={canChallenge ? challenge : undefined}
      now={now}
    />
  );
  const noteCard = (t: EntryThread, open: boolean) => (
    <NoteCard
      key={t.entry.id}
      thread={t}
      open={open}
      openLabel={`open: the next agent to start a ticket in this ${noun} gets it`}
      agents={agents}
      byId={byId}
      now={now}
    />
  );

  const loadNote = failed ? (
    <EntryHint>Entries could not be loaded.</EntryHint>
  ) : value === null ? (
    <EntryHint>Loading…</EntryHint>
  ) : null;
  const decisionsReplaced = groups.decisions.reduce((n, t) => n + t.replaced.length, 0);

  return (
    // Focusable from script only: a link to the entries moves focus here.
    <div
      ref={rootRef}
      tabIndex={-1}
      role="region"
      aria-label={`${noun === "epic" ? "Epic" : "Project"} entries`}
      data-testid="level-entries"
      className="space-y-6 focus:outline-none"
    >
      <EntrySection title="Notes" count={groups.openNotes.length > 0 ? `${groups.openNotes.length} open` : undefined}>
        {/* The box still works when the read failed, so only the failure is said here. */}
        {failed && <EntryHint>Entries could not be loaded.</EntryHint>}
        {groups.openNotes.length > 0 && <EntryStack>{groups.openNotes.map((t) => noteCard(t, true))}</EntryStack>}
        {groups.handledNotes.length > 0 && (
          <EntryFold label={`${groups.handledNotes.length} handled`}>
            <PagedEntries
              threads={groups.handledNotes}
              noun="handled notes"
              one="handled note"
              render={(t) => noteCard(t, false)}
            />
          </EntryFold>
        )}
        {!readOnly && (
          <NoteBox
            owner={ref}
            placeholder={PLACEHOLDER[noun]}
            about={about}
            focusSeq={focusSeq}
            onClearAbout={clear}
            onLeft={() => {
              clear();
              reload();
            }}
          />
        )}
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
            <PagedEntries threads={groups.decisions} noun="decisions" one="decision" render={(t) => card(t, true)} />
          ) : (
            <EntryHint>No decisions yet.</EntryHint>
          ))}
      </EntrySection>

      <EntrySection title="Learnings" count={groups.learnings.length > 0 ? String(groups.learnings.length) : undefined}>
        {loadNote ??
          (groups.learnings.length > 0 ? (
            <PagedEntries threads={groups.learnings} noun="learnings" one="learning" render={(t) => card(t, true)} />
          ) : (
            <EntryHint>No learnings yet.</EntryHint>
          ))}
      </EntrySection>

      {groups.other.length > 0 && (
        <EntrySection title="Other entries" count={String(groups.other.length)}>
          <PagedEntries threads={groups.other} noun="entries" one="entry" render={(t) => card(t, false)} />
        </EntrySection>
      )}
    </div>
  );
}
