import { Calendar, Check, CheckCircle2, EyeOff } from "lucide-react";
import type { Ticket } from "../api/client";
import type { GraphNode } from "../lib/graphLayout";
import { hiddenBlockersText, satisfiedDependenciesText } from "../lib/graphText";
import { STATUS_COLORS, STATUS_LABELS, isStatus } from "../lib/status";
import DependencyBand from "./DependencyBand";
import PriorityBadge from "./PriorityBadge";

/** What the graph knows about a card's dependencies, beyond the ticket itself. */
export type GraphCardInfo = Pick<GraphNode, "satisfiedDependencyCount" | "externalBlockerCount">;

function SubtaskProgress({ subtasks }: { subtasks: Ticket["subtasks"] }) {
  if (!subtasks || subtasks.length === 0) return null;
  const done = subtasks.filter((s) => s.completed).length;
  const pct = Math.round((done / subtasks.length) * 100);
  return (
    <div className="flex items-center gap-2 text-xs text-slate-500">
      <CheckCircle2 className="w-3 h-3" />
      <div className="flex-1 h-1 rounded-full bg-slate-700 overflow-hidden">
        <div
          className="h-full bg-blue-500 rounded-full transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
      <span>
        {done}/{subtasks.length}
      </span>
    </div>
  );
}

function StatusDot({ status }: { status: string }) {
  const known = isStatus(status);
  return (
    <span
      title={known ? STATUS_LABELS[status] : status}
      className={`w-2 h-2 shrink-0 rounded-full ${known ? STATUS_COLORS[status] : "bg-slate-500"}`}
    />
  );
}

// Replaces the dependency band on the graph, where unfinished dependencies in
// view are already drawn as arrows: what's left to say is how many are done
// and how many block the ticket from off the page.
function GraphDependencies({ graph }: { graph: GraphCardInfo }) {
  const satisfied = satisfiedDependenciesText(graph.satisfiedDependencyCount);
  const hidden = hiddenBlockersText(graph.externalBlockerCount);
  if (!satisfied && !hidden) return null;
  return (
    <div className="space-y-1 text-[10.5px]">
      {satisfied && (
        <div className="flex items-center gap-1.5 text-slate-500">
          <Check className="w-3 h-3 text-green-500" />
          {satisfied}
        </div>
      )}
      {hidden && (
        <div
          title="Unfinished dependencies that aren't shown, such as tickets in another project"
          className="flex items-center gap-1.5 text-red-300"
        >
          <EyeOff className="w-3 h-3" />
          {hidden}
        </div>
      )}
    </div>
  );
}

/**
 * A ticket card, shared by the board and the graph. It is purely
 * presentational: the board wraps it for dragging, the graph positions it.
 * Passing `graph` turns it into the graph's card, which adds a status dot
 * before the key and swaps the dependency band for done and hidden-blocker
 * counts. Without it the card renders exactly as the board always has.
 */
export default function TicketCard({
  ticket,
  isDragging,
  onClick,
  graph,
}: {
  ticket: Ticket;
  isDragging?: boolean;
  onClick?: () => void;
  graph?: GraphCardInfo;
}) {
  return (
    <div
      onClick={onClick}
      className={`rounded-lg border border-slate-700/50 bg-slate-900 p-3 space-y-2 transition-colors hover:border-slate-600 cursor-pointer ${
        isDragging ? "opacity-90 shadow-xl shadow-blue-500/10 rotate-2" : ""
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        {graph ? (
          <span className="inline-flex items-center gap-1.5 text-[11px] font-mono text-slate-500">
            <StatusDot status={ticket.status} />
            {ticket.projectPrefix}-{ticket.number}
          </span>
        ) : (
          <span className="text-[11px] font-mono text-slate-500">
            {ticket.projectPrefix}-{ticket.number}
          </span>
        )}
        <PriorityBadge priority={ticket.priority} />
      </div>
      <p className="text-sm text-slate-200 leading-snug">{ticket.title}</p>
      {graph ? <GraphDependencies graph={graph} /> : <DependencyBand dependsOn={ticket.dependsOn} />}
      {ticket.labels && ticket.labels.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {ticket.labels.map((l) => (
            <span
              key={l.id}
              className="inline-flex items-center rounded px-1.5 py-0.5 text-[10.5px] font-medium"
              style={{ backgroundColor: l.color + "1f", color: l.color }}
            >
              {l.name}
            </span>
          ))}
        </div>
      )}
      {ticket.dueDate && (
        <div className="flex items-center gap-1.5 text-xs text-slate-500">
          <Calendar className="w-3 h-3" />
          {new Date(ticket.dueDate).toLocaleDateString()}
        </div>
      )}
      <SubtaskProgress subtasks={ticket.subtasks} />
    </div>
  );
}
