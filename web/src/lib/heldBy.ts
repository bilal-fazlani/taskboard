// What the ticket page's "Held by" field says about the agent holding it.
import { STATUS_COLORS, isStatus } from "./status";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** When an agent was last seen, as the field words it: "seen just now",
 * "seen 4m ago", "seen 2h ago", "seen 3d ago". An unreadable time says
 * nothing. */
export function seenAgo(at: string | undefined, now: number): string {
  const when = at ? Date.parse(at) : NaN;
  if (Number.isNaN(when)) return "";
  const ms = Math.max(0, now - when);
  if (ms < MINUTE) return "seen just now";
  if (ms < HOUR) return `seen ${Math.floor(ms / MINUTE)}m ago`;
  if (ms < DAY) return `seen ${Math.floor(ms / HOUR)}h ago`;
  return `seen ${Math.floor(ms / DAY)}d ago`;
}

/** The dot beside the holder: blue or violet while the agent works, red
 * while the ticket waits on the person. Full literal classes, for Tailwind. */
export function heldDotColor(status: string): string {
  if (isStatus(status)) return STATUS_COLORS[status];
  return status === "needs_user_input" ? "bg-red-500" : "bg-slate-500";
}
