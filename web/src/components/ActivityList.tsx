import { ArrowRight } from "lucide-react";
import type { StatusChange } from "../api/client";
import { activityTime, type ActivityEntry } from "../lib/activity";
import { STATUS_LABELS, STATUS_STYLES, isStatus } from "../lib/status";

function StatusBadge({ status }: { status: string }) {
  const style = isStatus(status) ? STATUS_STYLES[status] : "bg-slate-500/20 text-slate-400";
  const label = isStatus(status) ? STATUS_LABELS[status] : status.replace("_", " ");
  return (
    <span className={`shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium ${style}`}>
      {label}
    </span>
  );
}

function StatusChangeEntry({ change }: { change: StatusChange }) {
  return change.fromStatus ? (
    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
      <StatusBadge status={change.fromStatus} />
      <ArrowRight aria-label="to" className="h-3 w-3 shrink-0 text-slate-500" />
      <StatusBadge status={change.toStatus} />
    </span>
  ) : (
    <span className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-slate-400">
      Created in
      <StatusBadge status={change.toStatus} />
    </span>
  );
}

/**
 * The editor's Activity list, newest first. Each kind of entry renders its own
 * line; the time sits on the right and a note, when there is one, sits under
 * its entry.
 */
export default function ActivityList({ entries, now }: { entries: ActivityEntry[]; now?: Date }) {
  if (entries.length === 0) {
    return <p className="text-xs text-slate-600">No activity yet.</p>;
  }
  return (
    <ol className="space-y-2.5">
      {entries.map((entry) => {
        const note = entry.kind === "status" ? entry.change.note : "";
        return (
          <li key={`${entry.kind}-${entry.id}`} data-testid="activity-entry" className="px-2">
            <div className="flex items-center justify-between gap-3">
              {entry.kind === "status" && <StatusChangeEntry change={entry.change} />}
              <time
                dateTime={entry.at}
                title={new Date(entry.at).toLocaleString()}
                className="shrink-0 whitespace-nowrap text-[11px] text-slate-500"
              >
                {activityTime(entry.at, now)}
              </time>
            </div>
            {note && (
              <p
                data-testid="activity-note"
                className="ml-2 mt-1.5 whitespace-pre-wrap border-l-2 border-slate-700 pl-3 text-sm text-slate-300"
              >
                {note}
              </p>
            )}
          </li>
        );
      })}
    </ol>
  );
}
