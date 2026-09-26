// Formats a due date for display. The API returns dueDate as an RFC 3339
// timestamp at midnight UTC (e.g. "2026-10-01T00:00:00Z"). Building a `Date`
// from that and calling toLocaleDateString() on it converts through the
// viewer's local timezone first, so anyone west of UTC (e.g.
// America/Los_Angeles) sees the previous day: 30 Sep instead of 1 Oct.
//
// ticketFields.ts's toDateInputValue fixed the same problem for the editor's
// date input by reading the "YYYY-MM-DD" part of the string rather than
// building a Date. This does the same for read-only display: it reads that
// calendar date, builds a Date from its UTC fields, and formats it back with
// `timeZone: "UTC"` so the day never moves — the locale's usual format
// (e.g. "10/1/2026") is unchanged, only the timezone conversion is removed.
import { toDateInputValue } from "./ticketFields";

export function formatDueDate(dueDate?: string): string {
  const datePart = toDateInputValue(dueDate);
  if (!datePart) return "";
  const [year, month, day] = datePart.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day)).toLocaleDateString(undefined, {
    timeZone: "UTC",
  });
}
