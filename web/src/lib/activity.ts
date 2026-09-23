// The editor's Activity list: what happened to a ticket, newest first.
//
// Today every entry is a status change. The list is built from entries of a
// tagged kind rather than from status changes directly, so other kinds (such
// as comments) can join it later as another member of ActivityEntry, merged
// into the same newest-first order.

import type { StatusChange } from "../api/client";

export type ActivityEntry = { kind: "status"; id: string; at: string; change: StatusChange };

/** Activity entries for a ticket's status changes, newest first. */
export function activityEntries(changes: readonly StatusChange[]): ActivityEntry[] {
  return changes
    .map((change): ActivityEntry => ({ kind: "status", id: change.id, at: change.createdAt, change }))
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

const pad = (n: number) => String(n).padStart(2, "0");

/**
 * When an entry happened, in local time, as the list shows it: "today 20:12"
 * for today, and just the date for anything older ("16 Sep", with the year
 * when it is not this year's).
 */
export function activityTime(at: string, now: Date = new Date()): string {
  const when = new Date(at);
  if (Number.isNaN(when.getTime())) return "";
  const sameDay =
    when.getFullYear() === now.getFullYear() && when.getMonth() === now.getMonth() && when.getDate() === now.getDate();
  if (sameDay) return `today ${pad(when.getHours())}:${pad(when.getMinutes())}`;
  const date = `${when.getDate()} ${MONTHS[when.getMonth()]}`;
  return when.getFullYear() === now.getFullYear() ? date : `${date} ${when.getFullYear()}`;
}
