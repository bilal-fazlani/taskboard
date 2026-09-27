// How the ticket page reads a ticket's entries: grouped by type in reading
// order, each current entry with the entries it replaced tucked under it, and
// the entries the person has challenged picked out. Pure functions over what
// the entries route answers, so the page and its tests share one rule.
import type { Entry } from "../api/client";

/** A current entry and the entries it replaced, newest first: the one it
 * replaced directly, then the one that one replaced, and so on. */
export interface EntryThread {
  entry: Entry;
  replaced: Entry[];
}

/** A ticket's entries in the page's reading order. Each list is newest first. */
export interface TicketEntryGroups {
  openNotes: EntryThread[];
  handledNotes: EntryThread[];
  /** Where it stands: the newest current hand-off, and any older ones still current. */
  handOff: EntryThread | null;
  earlierHandOffs: EntryThread[];
  decisions: EntryThread[];
  reviews: EntryThread[];
  learnings: EntryThread[];
  proofs: EntryThread[];
  /** Types this page doesn't know yet (the set is open), so nothing is dropped. */
  other: EntryThread[];
}

const KNOWN_TYPES = new Set(["note", "hand_off", "decision", "review", "learning", "proof"]);

/** Whether a note is open: no agent has handled it and no later note replaced it (models.Entry.Open). */
export function isOpenNote(e: Entry): boolean {
  return e.type === "note" && !e.handledAt && !e.replacedBy;
}

/** Newest first, the way the entries route orders them; ties keep their order. */
function newestFirst(entries: readonly Entry[]): Entry[] {
  return [...entries].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
}

/**
 * Groups entries read with includeReplaced: every current entry (none has
 * replaced it) under its type, each with the chain of entries it replaced.
 * A replaced entry whose replacement isn't among `entries` is left out.
 */
export function groupTicketEntries(entries: readonly Entry[]): TicketEntryGroups {
  const byId = new Map(entries.map((e) => [e.id, e]));
  const threads = newestFirst(entries.filter((e) => !e.replacedBy)).map((entry): EntryThread => {
    const replaced: Entry[] = [];
    const seen = new Set([entry.id]);
    let next = entry.replaces ? byId.get(entry.replaces) : undefined;
    while (next && !seen.has(next.id)) {
      replaced.push(next);
      seen.add(next.id);
      next = next.replaces ? byId.get(next.replaces) : undefined;
    }
    return { entry, replaced };
  });
  const of = (type: string) => threads.filter((t) => t.entry.type === type);
  const handOffs = of("hand_off");
  return {
    openNotes: of("note").filter((t) => isOpenNote(t.entry)),
    handledNotes: of("note").filter((t) => !isOpenNote(t.entry)),
    handOff: handOffs[0] ?? null,
    earlierHandOffs: handOffs.slice(1),
    decisions: of("decision"),
    reviews: of("review"),
    learnings: of("learning"),
    proofs: of("proof"),
    other: threads.filter((t) => !KNOWN_TYPES.has(t.entry.type)),
  };
}

/** The open notes pointing at each entry, by the id of the entry they challenge. */
export function openChallenges(entries: readonly Entry[]): Map<string, Entry[]> {
  const out = new Map<string, Entry[]>();
  for (const e of entries) {
    if (!e.about || !isOpenNote(e)) continue;
    out.set(e.about, [...(out.get(e.about) ?? []), e]);
  }
  return out;
}

/**
 * A review's round: the number in its report document's name ("Review 2"),
 * or else its place among the ticket's reviews, oldest first.
 */
export function reviewRound(review: Entry, reviews: readonly Entry[]): number {
  const named = /^review\s+(\d+)/i.exec(review.reportDocument ?? "");
  if (named) return Number(named[1]);
  const oldestFirst = [...newestFirst(reviews)].reverse();
  return oldestFirst.findIndex((r) => r.id === review.id) + 1;
}

/** The severities findings are counted by, most severe first (models.ReviewSeverities). */
const SEVERITIES = ["blocker", "major", "minor", "nit"];

/** A review's findings as the card shows them: "0 blocker · 1 major · 2 minor", nits only when there are some. */
export function findingsSummary(findings: Record<string, number> | undefined): string {
  if (!findings) return "";
  return SEVERITIES.filter((s) => s !== "nit" || (findings[s] ?? 0) > 0)
    .map((s) => `${findings[s] ?? 0} ${s}`)
    .join(" · ");
}

/**
 * A decision's text split into what was chosen and what was rejected, at its
 * first "Rejected:", so the card can set the rejected part apart. Without
 * one, rejected is "".
 */
export function splitRejected(text: string): { chosen: string; rejected: string } {
  const at = text.search(/\bRejected:/);
  if (at <= 0) return { chosen: text, rejected: "" };
  return { chosen: text.slice(0, at).trim(), rejected: text.slice(at).trim() };
}

/** A resume command as its chip shows it: a UUID shortened to its first eight characters. */
export function shortResumeCommand(command: string): string {
  return command.replace(/\b([0-9a-f]{8})-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi, "$1");
}

/**
 * The first words of an entry, for a note that points at it: at most max
 * characters, cut at a word boundary when one falls in the second half.
 */
export function entryExcerpt(text: string, max = 60): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= max) return flat;
  const cut = flat.slice(0, max + 1);
  const space = cut.lastIndexOf(" ");
  return `${(space > max / 2 ? cut.slice(0, space) : flat.slice(0, max)).trimEnd()}…`;
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * An epic's or project's entries as the side column counts them: decisions
 * and learnings when there are some, open notes always.
 */
export function countsPhrase(c: { decisions: number; learnings: number; openNotes: number }): string {
  const parts: string[] = [];
  if (c.decisions > 0) parts.push(plural(c.decisions, "decision", "decisions"));
  if (c.learnings > 0) parts.push(plural(c.learnings, "learning", "learnings"));
  parts.push(plural(c.openNotes, "open note", "open notes"));
  return parts.join(", ");
}

/** An entry type's label, as its small tag shows it. */
export function entryTypeLabel(type: string): string {
  switch (type) {
    case "hand_off":
      return "Hand-off";
    case "decision":
      return "Decision";
    case "learning":
      return "Learning";
    case "proof":
      return "Proof";
    case "review":
      return "Review";
    case "note":
      return "Note";
    default:
      return type.replace(/_/g, " ");
  }
}
