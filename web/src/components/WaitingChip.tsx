import { Link } from "react-router-dom";
import { WAITING_LINK } from "../lib/waiting";

/**
 * "N waiting on you" in a page header: how many tickets, in every project,
 * wait on the person's answer, linking to Now's waiting group until the inbox
 * (ACP-156) exists. Nothing shows when nothing waits.
 */
export default function WaitingChip({ count }: { count: number }) {
  if (count <= 0) return null;
  // The accessible name starts with the visible text, so a voice command
  // that says what it sees finds it (WCAG 2.5.3), and says the count is
  // every project's, which a one-project view may not show.
  return (
    <Link
      to={WAITING_LINK}
      data-testid="waiting-chip"
      aria-label={`${count} waiting on you, in every project: open the waiting list`}
      className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-red-500/50 bg-red-500/10 py-0.5 pr-3 pl-2.5 text-xs font-semibold text-red-400 transition-colors hover:border-red-400 hover:bg-red-500/20 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-400"
    >
      <span aria-hidden="true" className="h-2 w-2 rounded-full bg-red-500" />
      <span className="tabular-nums">{count}</span> waiting on you
    </Link>
  );
}
