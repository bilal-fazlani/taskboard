// What the Epics view shows, as plain functions of the API's epic list.
//
// An epic groups some of a project's tickets. It has no status of its own: it
// is complete once it has tickets and every one is done, which the API works
// out (see EpicProgress). The view lists a project's epics with a progress bar
// each, the busy ones first, then a "No epic" row for the project's tickets
// outside any epic, and folds the complete epics away at the bottom.

import type { Epic, EpicProgress } from "../api/client";
import { NO_EPIC, isNoEpic } from "./filters";
import { ACTIVE_STATUSES, DONE_STATUS, type Status } from "./status";
import type { TicketViewPath } from "./lastView";

const count = (progress: EpicProgress, status: string) => progress.counts?.[status] ?? 0;

/** How many of the tickets an agent holds: in progress or in agent review. */
export function activeCount(progress: EpicProgress): number {
  return ACTIVE_STATUSES.reduce((sum, status) => sum + count(progress, status), 0);
}

/** How many of the tickets are done. */
export function doneCount(progress: EpicProgress): number {
  return count(progress, DONE_STATUS);
}

// Epics with an active ticket first, then the others with tickets, then the
// empty ones.
function rank(epic: Epic): number {
  if (activeCount(epic) > 0) return 0;
  return epic.total > 0 ? 1 : 2;
}

function activityTime(epic: Epic): number {
  const at = epic.lastActivityAt ? Date.parse(epic.lastActivityAt) : NaN;
  return Number.isNaN(at) ? -Infinity : at;
}

/**
 * The order the view lists epics in: those with an active ticket first, then
 * by their tickets' latest change, newest first, and on a tie by name ignoring
 * case. Empty epics, with no activity, come after every epic with tickets.
 */
export function compareEpics(a: Epic, b: Epic): number {
  return (
    rank(a) - rank(b) ||
    activityTime(b) - activityTime(a) ||
    a.name.toLowerCase().localeCompare(b.name.toLowerCase()) ||
    a.name.localeCompare(b.name)
  );
}

/**
 * The epics the view lists, in order: the live ones in the main list, and the
 * complete ones for the folded section at the bottom. An empty epic is never
 * complete, so it stays in the main list.
 */
export function arrangeEpics(epics: readonly Epic[]): { live: Epic[]; complete: Epic[] } {
  const sorted = [...epics].sort(compareEpics);
  return { live: sorted.filter((e) => !e.complete), complete: sorted.filter((e) => e.complete) };
}

/** Whether the No epic row shows: only when the project has a ticket without an epic. */
export function showsNoEpic(noEpic: EpicProgress | null | undefined): boolean {
  return (noEpic?.total ?? 0) > 0;
}

/** The progress bar's segments from left to right, in the status colours. */
export const BAR_ORDER: readonly Status[] = ["done", "agent_review", "in_progress", "todo"];

export interface BarSegment {
  status: Status;
  count: number;
  /** The segment's share of the bar, in percent. */
  percent: number;
}

/**
 * The bar's segments, each as wide as its share of the tickets. Statuses with
 * no tickets have none, and neither has an epic with no tickets: its bar is
 * empty.
 */
export function barSegments(progress: EpicProgress): BarSegment[] {
  if (progress.total <= 0) return [];
  return BAR_ORDER.map((status) => ({
    status,
    count: count(progress, status),
    percent: (count(progress, status) / progress.total) * 100,
  })).filter((segment) => segment.count > 0);
}

/** The count at the row's right: "3 / 7 done", or "0 tickets" for an epic with none. */
export function progressText(progress: EpicProgress): string {
  if (progress.total <= 0) return "0 tickets";
  return `${doneCount(progress)} / ${progress.total} done`;
}

/**
 * The link a row opens: the given ticket view showing only the project and
 * the epic (NO_EPIC for the No epic row). No other filter is carried over.
 */
export function epicLink(view: TicketViewPath, project: string, epic: string): string {
  return `${view}?${new URLSearchParams({ project, epic }).toString()}`;
}

/**
 * What is wrong with an epic name typed into the dialog, as the store words
 * it, or null when nothing is. `epics` are the project's epics, and
 * `editingId` the one being renamed, which may keep its own name in other
 * capitals.
 */
export function nameError(name: string, epics: readonly Epic[], editingId?: string): string | null {
  const trimmed = name.trim();
  if (trimmed === "") return "Enter a name";
  if (isNoEpic(trimmed)) return `"${NO_EPIC}" is reserved for tickets without an epic.`;
  const taken = epics.find((e) => e.id !== editingId && e.name.trim().toLowerCase() === trimmed.toLowerCase());
  if (taken) return `This project already has an epic called "${taken.name}".`;
  return null;
}

/** The delete confirmation's body: what happens to the epic's tickets. */
export function deleteMessage(total: number): string {
  if (total <= 0) return "It has no tickets.";
  if (total === 1) return "Its 1 ticket stays as it is and moves to No epic. This can't be undone.";
  return `Its ${total} tickets stay as they are and move to No epic. This can't be undone.`;
}

/** The API client's message shape, e.g. `API error 400: {"error":"…"}`. */
const API_ERROR = /^API error \d{3}: ?([\s\S]*)$/;

/**
 * The server's message for a request it refused, as the API worded it, or
 * `fallback` when there is none (the request never reached it, or the body
 * says nothing).
 */
export function serverMessage(error: unknown, fallback: string): string {
  const raw = error instanceof Error ? error.message : "";
  const body = API_ERROR.exec(raw)?.[1]?.trim() ?? "";
  if (body === "") return fallback;
  try {
    const parsed: unknown = JSON.parse(body);
    const message = (parsed as { error?: unknown } | null)?.error;
    return typeof message === "string" && message.trim() !== "" ? message : fallback;
  } catch {
    return body;
  }
}
