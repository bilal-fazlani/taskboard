import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Plus,
  ChevronRight,
  AlertTriangle,
  ArrowUp,
  ArrowRight,
  ArrowDown,
  Calendar,
  Ticket as TicketIcon,
} from "lucide-react";
import { api, type Ticket, type Project, type TicketWrite } from "../api/client";
import TicketEditor from "../components/TicketEditor";
import CreateTicketModal from "../components/CreateTicketModal";
import FilterPanel from "../components/FilterPanel";
import { useFilters } from "../hooks/useFilters";
import { matchesFilters, repoOptions } from "../lib/filters";
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

export default function Tickets() {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [selectedTicket, setSelectedTicket] = useState<Ticket | null>(null);

  const filterState = useFilters();
  const { filters } = filterState;

  const load = useCallback(async () => {
    try {
      const [t, p] = await Promise.all([api.tickets.list(), api.projects.list()]);
      setTickets(t || []);
      setProjects(p || []);
    } catch {
      setTickets([]);
      setProjects([]);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Filters apply client-side, so changing one never refetches.
  const filtered = useMemo(() => tickets.filter((t) => matchesFilters(t, filters)), [tickets, filters]);
  const repos = useMemo(() => repoOptions(tickets, filters.repo), [tickets, filters.repo]);

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
        <button
          onClick={() => setShowCreate(true)}
          className="inline-flex items-center gap-2 px-3.5 py-1.5 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Ticket
        </button>
      </header>

      <FilterPanel
        state={filterState}
        projects={projects}
        repos={repos}
        count={loading ? undefined : { shown: filtered.length, total: tickets.length }}
      />

      <div className="flex-1 overflow-auto">
        {loading ? (
          <div className="flex items-center justify-center h-64 text-slate-600">
            Loading tickets…
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-slate-600 space-y-3">
            <TicketIcon className="w-10 h-10 text-slate-700" />
            <p className="text-sm">
              {filterState.active && tickets.length > 0 ? "No tickets match the filters" : "No tickets found"}
            </p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-slate-800 text-xs text-slate-500 uppercase tracking-wider">
                <th className="text-left px-6 py-3 font-medium">Key</th>
                <th className="text-left px-6 py-3 font-medium">Title</th>
                <th className="text-left px-4 py-3 font-medium">Labels</th>
                <th className="text-left px-4 py-3 font-medium">Repos</th>
                <th className="text-left px-6 py-3 font-medium">Status</th>
                <th className="text-left px-6 py-3 font-medium">Priority</th>
                <th className="text-left px-6 py-3 font-medium">Due</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {filtered.map((ticket) => (
                <tr
                  key={ticket.id}
                  onClick={() => setSelectedTicket(ticket)}
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
                        {new Date(ticket.dueDate).toLocaleDateString()}
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
          projects={projects}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}

      {selectedTicket && (
        <TicketEditor
          ticket={selectedTicket}
          projects={projects}
          onClose={() => {
            setSelectedTicket(null);
            load();
          }}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
        />
      )}
    </div>
  );
}
