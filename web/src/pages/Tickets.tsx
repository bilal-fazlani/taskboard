import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Plus,
  ChevronRight,
  AlertTriangle,
  ArrowUp,
  ArrowRight,
  ArrowDown,
  Calendar,
  Layers,
  ArrowUpDown,
  Ticket as TicketIcon,
} from "lucide-react";
import { api, type Ticket, type Project, type TicketWrite } from "../api/client";
import TicketEditor from "../components/TicketEditor";
import CreateTicketModal from "../components/CreateTicketModal";
import FilterPanel from "../components/FilterPanel";
import ProjectsLoadError from "../components/ProjectsLoadError";
import { useDocumentMatches } from "../hooks/useDocumentMatches";
import { useFilters } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { useTicketParam } from "../hooks/useTicketParam";
import { awaitingProject } from "../lib/defaultProject";
import { formatDueDate } from "../lib/dueDate";
import { nextEpicSort, sortByEpic, type EpicSort } from "../lib/epicSort";
import { inProject, matchesFilters, repoOptions } from "../lib/filters";
import { newTicketBlocked } from "../lib/newTicketDefaults";
import { STATUS_LABELS, STATUS_STYLES, isStatus, isDone } from "../lib/status";

const PRIORITY_CONFIG: Record<string, { style: string; icon: typeof ArrowUp }> = {
  urgent: { style: "bg-red-500/20 text-red-400", icon: AlertTriangle },
  high: { style: "bg-orange-500/20 text-orange-400", icon: ArrowUp },
  medium: { style: "bg-yellow-500/20 text-yellow-400", icon: ArrowRight },
  low: { style: "bg-green-500/20 text-green-400", icon: ArrowDown },
};

function StatusBadge({ status }: { status: string }) {
  const style = isStatus(status) ? STATUS_STYLES[status] : undefined;
  const label = isStatus(status) ? STATUS_LABELS[status] : status;
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium capitalize ${
        style || "bg-slate-700 text-slate-300"
      }`}
    >
      {label}
    </span>
  );
}

function PriorityBadge({ priority }: { priority: string }) {
  const config = PRIORITY_CONFIG[priority];
  if (!config) return null;
  const Icon = config.icon;
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium capitalize ${config.style}`}
    >
      <Icon className="w-3 h-3" />
      {priority}
    </span>
  );
}

// The Epic column's header, which sorts the table by epic. It steps through
// ascending, descending and back to the table's own order.
function EpicHeader({ sort, onSort }: { sort: EpicSort; onSort: () => void }) {
  const Icon = sort === "asc" ? ArrowUp : sort === "desc" ? ArrowDown : ArrowUpDown;
  return (
    <th
      aria-sort={sort === "asc" ? "ascending" : sort === "desc" ? "descending" : "none"}
      className="text-left px-4 py-3 font-medium"
    >
      <button
        type="button"
        onClick={onSort}
        className="inline-flex items-center gap-1 uppercase tracking-wider hover:text-slate-300 transition-colors"
      >
        Epic
        <Icon aria-hidden="true" className={`w-3 h-3 ${sort ? "text-slate-300" : "text-slate-600"}`} />
      </button>
    </th>
  );
}

export default function Tickets() {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  // Null until FilterPanel's own load (below) reports the first one back, and
  // whether that load has failed, through onProjects — the bar is the only
  // place that loads the projects list, so the table and the bar never
  // disagree about it (ACP-70).
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectsFailed, setProjectsFailed] = useState(false);
  const onProjects = useCallback((loaded: Project[] | null, failed: boolean) => {
    setProjects(loaded);
    setProjectsFailed(failed);
  }, []);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [epicSort, setEpicSort] = useState<EpicSort>(null);

  const filterState = useFilters();
  const { filters } = filterState;
  const docMatches = useDocumentMatches(filters.q, filters.project);
  // The open ticket comes from the URL, so ?ticket=KEY opens it on load.
  const {
    selected: selectedTicket,
    deleted: ticketDeleted,
    closeRequested,
    open: openTicket,
    switchTo: switchTicket,
    close: closeTicket,
    cancelClose,
    onDirtyChange,
    url: ticketUrl,
  } = useTicketParam(tickets);

  const load = useCallback(async () => {
    try {
      const t = await api.tickets.list();
      setTickets(t || []);
    } catch {
      // A failed refetch keeps what is on screen, so an open editor stays open.
      // The first load has nothing to keep and falls through to the empty state.
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // A live update only replaces the rows. `load` never sets `loading` back to
  // true, so the table isn't torn down and its scroll position stays; the
  // filters and the open editor both live in the URL, so neither notices —
  // the editor is handed the refreshed ticket and stays where it is.
  useLiveRefresh(load);

  // The table always shows one project, so its rows and count are out of that
  // project's tickets; without one (no active project to pick) it has none.
  // Filters apply client-side, so changing one never refetches.
  const projectTickets = useMemo(() => inProject(tickets, filters.project), [tickets, filters.project]);
  const filtered = useMemo(
    () => projectTickets.filter((t) => matchesFilters(t, filters, docMatches)),
    [projectTickets, filters, docMatches],
  );
  const rows = useMemo(() => sortByEpic(filtered, epicSort), [filtered, epicSort]);
  const projectCount = projectTickets.length;
  // The filter bar picks a project for a URL without one; until it has, the
  // table waits rather than listing every project's tickets. A projects load
  // that has never succeeded stops that wait rather than hanging on it
  // forever (ACP-70): with no project in the URL either, there is no way to
  // tell whether one would have been picked, so the table explains rather
  // than showing what would otherwise look like a project with no tickets.
  const projectsErrored = filters.project === "" && projects === null && projectsFailed;
  const waiting = loading || awaitingProject(filters.project, projects, projectsFailed);
  const repos = useMemo(() => repoOptions(projectTickets, filters.repo), [projectTickets, filters.repo]);
  // Why New Ticket can't open the form here, if it can't (see newTicketBlocked).
  const createBlocked = newTicketBlocked(filters.project, projects, projectsFailed);

  const handleCreate = async (data: TicketWrite) => {
    await api.tickets.create(data);
    setShowCreate(false);
    load();
  };

  const handleUpdate = async (id: string, data: TicketWrite) => {
    await api.tickets.update(id, data);
    load();
  };

  const handleDelete = async (id: string) => {
    await api.tickets.delete(id);
    load();
  };

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex items-center justify-between px-6 h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Table</h1>
        {/* aria-disabled rather than disabled, so the reason shows on hover and
            the button stays focusable for a screen reader to announce it. */}
        <button
          type="button"
          aria-disabled={createBlocked !== null || undefined}
          title={createBlocked ?? undefined}
          onClick={() => {
            if (createBlocked === null) setShowCreate(true);
          }}
          className="inline-flex items-center gap-2 px-3.5 py-1.5 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:bg-blue-600"
        >
          <Plus className="w-4 h-4" />
          New Ticket
        </button>
      </header>

      <FilterPanel
        state={filterState}
        tickets={loading ? null : tickets}
        repos={repos}
        count={waiting || projectsErrored ? undefined : { shown: filtered.length, total: projectCount }}
        onProjects={onProjects}
      />

      <div data-testid="table-scroll" className="flex-1 overflow-auto">
        {waiting ? (
          <div className="flex items-center justify-center h-64 text-slate-600">
            Loading tickets…
          </div>
        ) : projectsErrored ? (
          <ProjectsLoadError />
        ) : filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-slate-600 space-y-3">
            <TicketIcon className="w-10 h-10 text-slate-700" />
            <p className="text-sm">
              {filterState.active && projectCount > 0 ? "No tickets match the filters" : "No tickets found"}
            </p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-slate-800 text-xs text-slate-500 uppercase tracking-wider">
                <th className="text-left px-6 py-3 font-medium">Key</th>
                <th className="text-left px-6 py-3 font-medium">Title</th>
                <EpicHeader sort={epicSort} onSort={() => setEpicSort(nextEpicSort)} />
                <th className="text-left px-4 py-3 font-medium">Labels</th>
                <th className="text-left px-4 py-3 font-medium">Repos</th>
                <th className="text-left px-6 py-3 font-medium">Status</th>
                <th className="text-left px-6 py-3 font-medium">Priority</th>
                <th className="text-left px-6 py-3 font-medium">Due</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {rows.map((ticket) => (
                <tr
                  key={ticket.id}
                  onClick={() => openTicket(ticket)}
                  className="border-b border-slate-800/50 hover:bg-slate-900/50 cursor-pointer transition-colors"
                >
                  <td className="px-6 py-3">
                    <span className="text-xs font-mono text-slate-500">
                      {ticket.projectPrefix}-{ticket.number}
                    </span>
                  </td>
                  <td className="px-6 py-3">
                    <span className="text-sm text-slate-200">
                      {ticket.title}
                    </span>
                    {ticket.dependsOn && ticket.dependsOn.length > 0 && (
                      <div
                        className={`text-[10.5px] mt-0.5 ${
                          ticket.dependsOn.some((d) => !isDone(d.status))
                            ? "text-red-400"
                            : "text-slate-500"
                        }`}
                      >
                        depends on{" "}
                        <span className="font-mono">
                          {ticket.dependsOn.map((d) => d.key).join(", ")}
                        </span>
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {ticket.epic ? (
                      <span
                        title={ticket.epic.name}
                        className="inline-flex max-w-[12rem] items-center gap-1 text-xs text-slate-400"
                      >
                        <Layers aria-hidden="true" className="w-3 h-3 shrink-0 text-slate-500" />
                        <span className="truncate">{ticket.epic.name}</span>
                      </span>
                    ) : (
                      <span className="text-xs text-slate-700">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(ticket.labels || []).map((l) => (
                        <span
                          key={l.id}
                          className="inline-flex items-center rounded px-1.5 py-0.5 text-[10.5px] font-medium"
                          style={{ backgroundColor: l.color + "1f", color: l.color }}
                        >
                          {l.name}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(ticket.repos || []).map((repo) => (
                        <span
                          key={repo}
                          className="inline-flex items-center rounded bg-slate-800 px-1.5 py-0.5 font-mono text-[10.5px] text-slate-400"
                        >
                          {repo}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-6 py-3">
                    <StatusBadge status={ticket.status} />
                  </td>
                  <td className="px-6 py-3">
                    <PriorityBadge priority={ticket.priority} />
                  </td>
                  <td className="px-6 py-3">
                    {ticket.dueDate ? (
                      <span className="inline-flex items-center gap-1 text-xs text-slate-500">
                        <Calendar className="w-3 h-3" />
                        {formatDueDate(ticket.dueDate)}
                      </span>
                    ) : (
                      <span className="text-xs text-slate-700">—</span>
                    )}
                  </td>
                  <td className="px-3 py-3">
                    <ChevronRight className="w-4 h-4 text-slate-700" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {showCreate && (
        <CreateTicketModal
          projects={projects ?? []}
          filters={filters}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}

      {selectedTicket && (
        <TicketEditor
          key={selectedTicket.id}
          ticket={selectedTicket}
          projects={projects ?? []}
          ticketUrl={ticketUrl}
          deleted={ticketDeleted}
          closeRequested={closeRequested}
          onCloseCancelled={cancelClose}
          onDirtyChange={onDirtyChange}
          onClose={() => {
            closeTicket();
            load();
          }}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
          onOpenTicket={switchTicket}
        />
      )}
    </div>
  );
}
