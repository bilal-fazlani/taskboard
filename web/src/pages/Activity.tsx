import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Project, type ProjectActivityEntry, type Ticket, type TicketWrite } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import FilterPanel from "../components/FilterPanel";
import ProjectsLoadError from "../components/ProjectsLoadError";
import TicketEditor from "../components/TicketEditor";
import { useFilters } from "../hooks/useFilters";
import { useLiveRefresh, useLiveStatus, type LiveStatus } from "../hooks/useLiveRefresh";
import { useTicketParam } from "../hooks/useTicketParam";
import { entryTicket } from "../lib/activityFeed";
import { activeProjects, namedProject } from "../lib/defaultProject";

const NO_REPOS: string[] = [];

const LIVE_MARKERS: Record<LiveStatus, { label: string; title: string; text: string; dot: string }> = {
  live: {
    label: "Live",
    title: "New changes appear as they happen",
    text: "text-green-400",
    dot: "bg-green-400 ring-green-400/15",
  },
  connecting: {
    label: "Connecting…",
    title: "Connecting to the board for live changes",
    text: "text-slate-500",
    dot: "bg-slate-500 ring-slate-500/15",
  },
  reconnecting: {
    label: "Reconnecting…",
    title: "The connection to the board dropped: new changes show once it is back",
    text: "text-amber-400",
    dot: "bg-amber-400 ring-amber-400/15",
  },
};

/** Whether the feed is live: the state of the stream useLiveRefresh keeps open. */
function LiveMarker() {
  const marker = LIVE_MARKERS[useLiveStatus()];
  return (
    <span
      data-testid="live-marker"
      role="status"
      title={marker.title}
      className={`ml-auto inline-flex items-center gap-1.5 text-[11px] ${marker.text}`}
    >
      <span aria-hidden="true" className={`h-1.5 w-1.5 rounded-full ring-[3px] ${marker.dot}`} />
      {marker.label}
    </span>
  );
}

/**
 * Activity: every status change in the filter bar's project, newest first,
 * narrowed by epic only. Clicking an entry opens its ticket in the editor, as
 * a card does on the other views.
 *
 * The page loads every ticket, as the views do, for the filter bar's project
 * pick and for the editor; the feed itself reads its own pages (ActivityFeed).
 */
export default function Activity() {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [loading, setLoading] = useState(true);
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectsFailed, setProjectsFailed] = useState(false);
  const onProjects = useCallback((loaded: Project[] | null, failed: boolean) => {
    setProjects(loaded);
    setProjectsFailed(failed);
  }, []);

  const filterState = useFilters();
  const { filters } = filterState;
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

  const load = useCallback(() => {
    api.tickets
      .list()
      .then((t) => setTickets(t || []))
      // A failed refetch keeps what is on screen, so an open editor stays open.
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);
  useEffect(() => load(), [load]);
  useLiveRefresh(load);

  // The project the feed shows: the URL's, as the projects list spells it,
  // once the list says it exists (the bar replaces one that doesn't). Without
  // the list, after a failed load, the URL's is taken as it stands.
  const project = useMemo(() => {
    if (projects) return namedProject(projects.map((p) => p.prefix), filters.project);
    return projectsFailed && filters.project ? filters.project : null;
  }, [projects, projectsFailed, filters.project]);
  const projectsErrored = filters.project === "" && projects === null && projectsFailed;
  // Until then the page waits, as the views do: for the list, or for the bar
  // to pick or replace the URL's project, which it does whenever there is an
  // active project to pick.
  const waiting = projects === null ? !projectsFailed : activeProjects(projects).length > 0;

  const open = (entry: ProjectActivityEntry) =>
    openTicket(tickets.find((t) => t.id === entry.ticketId) ?? entryTicket(entry));

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex flex-wrap items-center gap-x-4 gap-y-2 px-6 py-2.5 min-h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Activity</h1>
        <FilterPanel
          epicOnly
          state={filterState}
          tickets={loading ? null : tickets}
          repos={NO_REPOS}
          onProjects={onProjects}
        />
        <LiveMarker />
      </header>

      <div data-testid="activity-scroll" className="flex-1 overflow-auto">
        {projectsErrored ? (
          <ProjectsLoadError />
        ) : project ? (
          <ActivityFeed
            key={`${project}\n${filters.epic.join("\n")}`}
            project={project}
            epics={filters.epic}
            onOpen={open}
          />
        ) : waiting ? (
          <div className="flex items-center justify-center h-64 text-slate-600">Loading activity…</div>
        ) : (
          <div className="flex items-center justify-center h-64 text-sm text-slate-600">No project to show</div>
        )}
      </div>

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
          onUpdate={async (id: string, data: TicketWrite) => {
            await api.tickets.update(id, data);
            load();
          }}
          onDelete={async (id: string) => {
            await api.tickets.delete(id);
            load();
          }}
          onOpenTicket={switchTicket}
        />
      )}
    </div>
  );
}
