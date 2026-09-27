// The Activity page: every status change in one project, ticket creation
// included, newest first and grouped by day. Pure functions over what the
// API returns; the page and components/ActivityFeed.tsx do the fetching and
// the drawing.

import type { ActivityPage, ProjectActivityEntry } from "../api/client";
import { MONTHS, pad } from "./activity";
import type { GlowTicket } from "./changeGlow";

/** How many entries the feed loads at first, and "Show older" adds. */
export const FEED_PAGE_SIZE = 50;

/** The most entries the API returns in one page (db.ActivityMaxLimit). */
export const FEED_MAX_LIMIT = 200;

/**
 * The class an entry a live refresh brought in wears while it glows, for
 * changeGlow's GLOW_MS. Plain CSS in src/index.css, like the board's
 * `graph-changed`.
 */
export const FEED_FRESH_CLASS = "activity-fresh";

const WEEKDAYS =["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const sameDay = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();

/**
 * The heading of the day an entry happened on, in local time: "Today",
 * "Yesterday", and otherwise the weekday and date ("Wed 23 Sep", with the
 * year when it is not this year's).
 */
export function feedDay(at: string, now: Date = new Date()): string {
  const when = new Date(at);
  if (Number.isNaN(when.getTime())) return "";
  if (sameDay(when, now)) return "Today";
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (sameDay(when, yesterday)) return "Yesterday";
  const date = `${WEEKDAYS[when.getDay()]} ${when.getDate()} ${MONTHS[when.getMonth()]}`;
  return when.getFullYear() === now.getFullYear() ? date : `${date} ${when.getFullYear()}`;
}

/** The time of day an entry happened, in local time, as "14:02". */
export function feedClock(at: string): string {
  const when = new Date(at);
  if (Number.isNaN(when.getTime())) return "";
  return `${pad(when.getHours())}:${pad(when.getMinutes())}`;
}

export interface FeedDay {
  label: string;
  entries: ProjectActivityEntry[];
}

/**
 * The entries under their day's heading, in the order given (newest first
 * from the API): each run of entries on the same day is one group.
 */
export function groupByDay(entries: readonly ProjectActivityEntry[], now: Date = new Date()): FeedDay[] {
  const days: FeedDay[] = [];
  for (const entry of entries) {
    const label = feedDay(entry.createdAt, now);
    const last = days[days.length - 1];
    if (last && last.label === label) last.entries.push(entry);
    else days.push({ label, entries: [entry] });
  }
  return days;
}

export interface NotePart {
  text: string;
  /** Whether the text is a commit sha, which the feed sets in monospace. */
  sha: boolean;
}

// A short or full sha: 7 to 40 lowercase hex characters standing alone, with
// at least one digit and one letter so a plain number or a word like
// "decade" isn't taken for one.
const SHA = /\b[0-9a-f]{7,40}\b/g;
const looksLikeSha = (word: string) => /[0-9]/.test(word) && /[a-f]/.test(word);

/** A note split into plain text and the commit shas in it, in order. */
export function noteParts(note: string): NotePart[] {
  const parts: NotePart[] = [];
  let from = 0;
  for (const match of note.matchAll(SHA)) {
    if (!looksLikeSha(match[0])) continue;
    const at = match.index ?? 0;
    if (at > from) parts.push({ text: note.slice(from, at), sha: false });
    parts.push({ text: match[0], sha: true });
    from = at + match[0].length;
  }
  if (from < note.length) parts.push({ text: note.slice(from), sha: false });
  return parts;
}

/** Reads one page: older than `before` when given, up to `limit` entries. */
export type FetchActivityPage = (before: string | undefined, limit: number) => Promise<ActivityPage>;

/**
 * The newest entries down to `through`, the oldest one the feed shows now,
 * and at least a first page's worth, read in pages of up to `count` (as many
 * as are shown) within what the API allows. A live refresh reads the feed
 * again this way, so what was loaded with "Show older" stays however many
 * new entries arrived above it, and a ticket deleted or renamed meanwhile is
 * reflected all the way down. Once `through` is gone (its ticket was
 * deleted), reading stops at the first entry older than it.
 */
export async function readNewest(
  fetchPage: FetchActivityPage,
  count: number,
  through?: Pick<ProjectActivityEntry, "id" | "createdAt">,
): Promise<ActivityPage> {
  const want = Math.max(count, FEED_PAGE_SIZE);
  const throughAt = through ? Date.parse(through.createdAt) : NaN;
  const entries: ProjectActivityEntry[] = [];
  let reached = !through;
  let before: string | undefined;
  for (;;) {
    const page = await fetchPage(before, Math.min(Math.max(want - entries.length, FEED_PAGE_SIZE), FEED_MAX_LIMIT));
    entries.push(...page.entries);
    if (!page.hasMore || !page.nextBefore) return { entries, hasMore: false };
    reached ||=
      page.entries.some((e) => e.id === through?.id) || Date.parse(entries[entries.length - 1].createdAt) < throughAt;
    if (entries.length >= want && reached) return { entries, hasMore: true, nextBefore: page.nextBefore };
    before = page.nextBefore;
  }
}

/** The newest entry of the feed's first load, or null when it had none. */
export type FeedBaseline = Pick<ProjectActivityEntry, "id" | "createdAt"> | null;

/**
 * The entries a live refresh can have brought in, for useChangeGlow: those
 * above the first load's newest entry, and that entry itself, which is
 * already on screen and so never glows. Older pages loaded with "Show older"
 * are below it and never count as new; neither does an older entry that
 * moves up when a ticket is deleted. When that newest entry is itself gone,
 * whatever is newer than it counts. With no baseline (the feed started
 * empty) every entry counts, so the first ones to arrive glow.
 */
export function sinceBaseline(entries: readonly ProjectActivityEntry[], baseline: FeedBaseline): GlowTicket[] {
  let recent: readonly ProjectActivityEntry[] = entries;
  if (baseline) {
    const index = entries.findIndex((e) => e.id === baseline.id);
    const at = Date.parse(baseline.createdAt);
    recent = index >= 0 ? entries.slice(0, index + 1) : entries.filter((e) => Date.parse(e.createdAt) > at);
  }
  return recent.map((e) => ({ id: e.id, updatedAt: e.createdAt }));
}

/** The ticket an entry belongs to, as the ticket parameter and the editor name it. */
export function entryTicket(entry: Pick<ProjectActivityEntry, "ticketId" | "ticketKey">): {
  id: string;
  number: number;
  projectPrefix: string;
} {
  const dash = entry.ticketKey.lastIndexOf("-");
  return {
    id: entry.ticketId,
    number: Number(entry.ticketKey.slice(dash + 1)),
    projectPrefix: dash > 0 ? entry.ticketKey.slice(0, dash) : "",
  };
}
