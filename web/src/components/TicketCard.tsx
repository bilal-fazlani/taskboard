import { Calendar, Check, CheckCircle2, EyeOff, Layers, Paperclip } from "lucide-react";
import type { EpicRef, Ticket } from "../api/client";
import { attentionClasses } from "../lib/attention";
import type { GraphNode } from "../lib/graphLayout";
import { hiddenBlockersText, satisfiedDependenciesText } from "../lib/graphText";
import { AGENT_REVIEW_STATUS, STATUS_COLORS, STATUS_LABELS, STATUS_STYLES, isStatus } from "../lib/status";
import DependencyBand from "./DependencyBand";
import PriorityBadge from "./PriorityBadge";

/** What the graph knows about a card's dependencies, beyond the ticket itself. */
export type GraphCardInfo = Pick<GraphNode, "satisfiedDependencyCount" | "externalBlockerCount" | "dependencyTotal">;

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

// Subtask progress and, beside it, a paperclip with how many documents the
// ticket has. Either can be missing; with neither, nothing shows.
function CardFooter({ ticket }: { ticket: Ticket }) {
  const documents = ticket.documentCount ?? 0;
  const hasSubtasks = (ticket.subtasks?.length ?? 0) > 0;
  if (!hasSubtasks && documents === 0) return null;
  return (
    <div className="flex items-center gap-3">
      <div className="min-w-0 flex-1">
        <SubtaskProgress subtasks={ticket.subtasks} />
      </div>
      {documents > 0 && (
        <span
          data-testid="card-documents"
          title={`${documents} document${documents === 1 ? "" : "s"}`}
          className="inline-flex shrink-0 items-center gap-1 text-xs text-slate-500"
        >
          <Paperclip aria-hidden="true" className="h-3 w-3" />
          {documents}
        </span>
      )}
    </div>
  );
}

function StatusDot({ status, attention }: { status: string; attention: string }) {
  const known = isStatus(status);
  return (
    <span
      title={known ? STATUS_LABELS[status] : status}
      className={`w-2 h-2 shrink-0 rounded-full ${known ? STATUS_COLORS[status] : "bg-slate-500"} ${attention}`}
    />
  );
}

// The epic before the key in the card's header: a neutral icon and the name,
// which is cut short at a fixed width and shown in full on hover. Epics have
// no colour of their own, so it stays the key's grey.
function EpicCrumb({ epic }: { epic: EpicRef }) {
  return (
    <span data-testid="card-epic" title={epic.name} className="inline-flex min-w-0 max-w-[8rem] items-center gap-1">
      <Layers aria-hidden="true" className="h-3 w-3 shrink-0" />
      <span className="truncate">{epic.name}</span>
    </span>
  );
}

// Replaces the dependency band on the graph, where unfinished dependencies in
// view are already drawn as arrows: what's left to say is how many are done
// and how many block the ticket from off the page.
function GraphDependencies({ graph }: { graph: GraphCardInfo }) {
  const satisfied = satisfiedDependenciesText(graph.satisfiedDependencyCount, graph.dependencyTotal);
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

// How many times the ticket has gone to the review agent, on the graph only.
// It wears the Agent Review badge's colours and stays once the ticket is
// done, since the rounds are part of how the ticket got there.
function ReviewRounds({ rounds }: { rounds: number }) {
  return (
    <div>
      <span
        data-testid="card-review-rounds"
        title={`Entered Agent Review ${rounds} ${rounds === 1 ? "time" : "times"}`}
        className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10.5px] font-medium ${STATUS_STYLES[AGENT_REVIEW_STATUS]}`}
      >
        review ×{rounds}
      </span>
    </div>
  );
}

/**
 * A ticket card, shared by the board and the graph. It is purely
 * presentational: the board wraps it for dragging, the graph positions it.
 * Passing `graph` turns it into the graph's card, which adds a status dot
 * before the key and swaps the dependency band for done and hidden-blocker
 * counts, and adds a "review ×N" pill under the title once the ticket has
 * been to agent review. Without it the card renders exactly as the board
 * always has. A ticket in an epic shows the epic before its key, after the
 * graph's dot.
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
  // Empty strings everywhere but an in-progress card on the graph.
  const attention = attentionClasses(ticket.status, graph !== undefined);
  const reviewRounds = ticket.reviewRounds ?? 0;
  return (
    <div
      onClick={onClick}
      className={`rounded-lg border border-slate-700/50 bg-slate-900 p-3 space-y-2 transition-colors hover:border-slate-600 cursor-pointer ${
        isDragging ? "opacity-90 shadow-xl shadow-blue-500/10 rotate-2" : ""
      } ${attention.card}`}
    >
      <div className="flex items-start justify-between gap-2">
        {ticket.epic ? (
          <span className="inline-flex min-w-0 items-center gap-1.5 text-[11px] text-slate-400">
            {graph && <StatusDot status={ticket.status} attention={attention.dot} />}
            <EpicCrumb epic={ticket.epic} />
            <span aria-hidden="true" className="shrink-0 text-slate-500">
              /
            </span>
            <span className="shrink-0 whitespace-nowrap font-mono">
              {ticket.projectPrefix}-{ticket.number}
            </span>
          </span>
        ) : graph ? (
          <span className="inline-flex items-center gap-1.5 text-[11px] font-mono text-slate-400">
            <StatusDot status={ticket.status} attention={attention.dot} />
            {ticket.projectPrefix}-{ticket.number}
          </span>
        ) : (
          <span className="text-[11px] font-mono text-slate-400">
            {ticket.projectPrefix}-{ticket.number}
          </span>
        )}
        <PriorityBadge priority={ticket.priority} />
      </div>
      <p className="text-sm text-slate-200 leading-snug">{ticket.title}</p>
      {graph && reviewRounds > 0 && <ReviewRounds rounds={reviewRounds} />}
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
      <CardFooter ticket={ticket} />
    </div>
  );
}
