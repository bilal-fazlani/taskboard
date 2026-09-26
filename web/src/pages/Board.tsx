import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  useDroppable,
  useDraggable,
  closestCorners,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { Plus } from "lucide-react";
import { api, type Ticket, type Project, type BoardColumn, type TicketWrite } from "../api/client";
import TicketEditor from "../components/TicketEditor";
import CreateTicketModal from "../components/CreateTicketModal";
import TicketCard from "../components/TicketCard";
import FilterPanel from "../components/FilterPanel";
import ProjectsLoadError from "../components/ProjectsLoadError";
import { useDocumentMatches } from "../hooks/useDocumentMatches";
import { useFilters } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { useTicketParam } from "../hooks/useTicketParam";
import { awaitingProject } from "../lib/defaultProject";
import { inProject, matchesFilters, repoOptions } from "../lib/filters";
import { newTicketBlocked } from "../lib/newTicketDefaults";
import { STATUSES, STATUS_LABELS, STATUS_COLORS, isStatus, type Status } from "../lib/status";

function DraggableTicket({
  ticket,
  onClick,
}: {
  ticket: Ticket;
  onClick: () => void;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: ticket.id,
    data: { ticket },
  });

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      className={`cursor-grab active:cursor-grabbing ${isDragging ? "opacity-30" : ""}`}
    >
      <TicketCard ticket={ticket} onClick={onClick} />
    </div>
  );
}

function Column({
  status,
  tickets,
  onTicketClick,
  onAddTicket,
  addBlocked,
}: {
  status: Status;
  tickets: Ticket[];
  onTicketClick: (ticket: Ticket) => void;
  onAddTicket: (status: string) => void;
  /** Why + can't open the new-ticket form, or null when it can (see newTicketBlocked). */
  addBlocked: string | null;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status });

  return (
    <div className="flex flex-col w-80 shrink-0">
      <div className="flex items-center gap-2 px-1 pb-3">
        <div className={`w-2 h-2 rounded-full ${STATUS_COLORS[status]}`} />
        <h3 className="text-sm font-medium text-slate-300">
          {STATUS_LABELS[status]}
        </h3>
        <span className="text-xs text-slate-600 ml-auto">{tickets.length}</span>
        {/* aria-disabled rather than disabled, so the reason shows on hover and
            the button stays focusable for a screen reader to announce it. */}
        <button
          type="button"
          aria-label={`New ticket in ${STATUS_LABELS[status]}`}
          aria-disabled={addBlocked !== null || undefined}
          title={addBlocked ?? undefined}
          onClick={() => {
            if (addBlocked === null) onAddTicket(status);
          }}
          className="text-slate-600 hover:text-slate-300 transition-colors aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:text-slate-600"
        >
          <Plus aria-hidden="true" className="w-4 h-4" />
        </button>
      </div>
      <div
        ref={setNodeRef}
        className={`flex-1 space-y-2 rounded-lg p-2 transition-colors min-h-32 ${
          isOver ? "bg-blue-500/5 ring-1 ring-blue-500/20" : ""
        }`}
      >
        {tickets.map((ticket) => (
          <DraggableTicket
            key={ticket.id}
            ticket={ticket}
            onClick={() => onTicketClick(ticket)}
          />
        ))}
        {tickets.length === 0 && (
          <div className="flex items-center justify-center h-24 text-xs text-slate-700 border border-dashed border-slate-800 rounded-lg">
            Drop tickets here
          </div>
        )}
      </div>
    </div>
  );
}

export default function Board() {
  // Null until FilterPanel's own load (below) reports the first one back, and
  // whether that load has failed, through onProjects — the bar is the only
  // place that loads the projects list, so the board and the bar never
  // disagree about it (ACP-70).
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectsFailed, setProjectsFailed] = useState(false);
  const onProjects = useCallback((loaded: Project[] | null, failed: boolean) => {
    setProjects(loaded);
    setProjectsFailed(failed);
  }, []);
  const [columns, setColumns] = useState<BoardColumn[]>([]);
  const [activeTicket, setActiveTicket] = useState<Ticket | null>(null);
  const [createForStatus, setCreateForStatus] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const filterState = useFilters();
  const { filters } = filterState;
  const docMatches = useDocumentMatches(filters.q, filters.project);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } })
  );

  // Live updates and the user's own edits both refetch, so two GETs can be in
  // flight at once and finish out of order. Only the newest one is allowed to
  // set the columns; an older response is dropped rather than putting stale
  // cards back on the board.
  const loadSeqRef = useRef(0);

  const loadBoard = useCallback(async () => {
    const seq = ++loadSeqRef.current;
    try {
      // Every project's tickets: the project is a filter, applied below.
      const board = await api.board.get();
      if (seq !== loadSeqRef.current) return;
      setColumns(board.columns || []);
    } catch {
      if (seq !== loadSeqRef.current) return;
      // A failed refetch keeps what is on screen — an open editor included —
      // and only an outright failed first load falls back to empty columns.
      setColumns((prev) => (prev.length > 0 ? prev : STATUSES.map((status) => ({ status, tickets: [] }))));
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    setLoading(true);
    loadBoard();
  }, [loadBoard]);

  // A live update replaces the columns without touching the scroll position,
  // which keeps its own state, or the open editor, which the URL names and a
  // refetch only refreshes. A drag is the exception: it
  // moves cards between columns optimistically, so swapping the columns under
  // it would yank the card away. The refetch waits for the drag to end.
  const draggingRef = useRef(false);
  const missedRefreshRef = useRef(false);
  const liveRefresh = useCallback(() => {
    // The projects come along through FilterPanel's own live refresh (below),
    // so the editor names a project renamed elsewhere.
    if (draggingRef.current) {
      missedRefreshRef.current = true;
      return;
    }
    loadBoard();
  }, [loadBoard]);
  useLiveRefresh(liveRefresh);

  // Called when a drag ends, however it ended.
  const endDrag = useCallback(() => {
    draggingRef.current = false;
    if (!missedRefreshRef.current) return;
    missedRefreshRef.current = false;
    loadBoard();
  }, [loadBoard]);

  // Non-matching tickets are left out. The ticket being dragged always stays,
  // so moving it into a column its status filter excludes doesn't unmount it
  // mid-drag.
  const allTickets = useMemo(() => columns.flatMap((c) => c.tickets), [columns]);
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
  } = useTicketParam(allTickets);
  // The board always shows one project, so its cards and count are out of that
  // project's tickets; without one (no active project to pick) it has none.
  const projectTickets = useMemo(() => inProject(allTickets, filters.project), [allTickets, filters.project]);
  const isShown = (ticket: Ticket) =>
    ticket.id === activeTicket?.id || (filters.project !== "" && matchesFilters(ticket, filters, docMatches));
  const getColumnTickets = (status: string) =>
    (columns.find((c) => c.status === status)?.tickets || []).filter(isShown);
  const shownCount = projectTickets.filter((t) => matchesFilters(t, filters, docMatches)).length;
  const projectCount = projectTickets.length;
  // The filter bar picks a project for a URL without one; until it has, the
  // board waits rather than showing every project's tickets. A projects load
  // that has never succeeded stops that wait rather than hanging on it
  // forever (ACP-70): with no project in the URL either, there is no way to
  // tell whether one would have been picked, so the board explains rather
  // than showing what would otherwise look like a project with no tickets.
  const projectsErrored = filters.project === "" && projects === null && projectsFailed;
  const waiting = loading || awaitingProject(filters.project, projects, projectsFailed);
  const repos = useMemo(() => repoOptions(projectTickets, filters.repo), [projectTickets, filters.repo]);
  const addBlocked = newTicketBlocked(filters.project, projects, projectsFailed);

  const findTicketById = (id: UniqueIdentifier): Ticket | undefined => {
    for (const col of columns) {
      const found = col.tickets.find((t) => t.id === id);
      if (found) return found;
    }
    return undefined;
  };

  const findColumnByTicketId = (id: UniqueIdentifier): string | undefined => {
    for (const col of columns) {
      if (col.tickets.find((t) => t.id === id)) return col.status;
    }
    return undefined;
  };

  const handleDragStart = (event: DragStartEvent) => {
    const ticket = findTicketById(event.active.id);
    draggingRef.current = true;
    setActiveTicket(ticket ?? null);
  };

  const handleDragOver = (event: DragOverEvent) => {
    const { active, over } = event;
    if (!over) return;

    const activeStatus = findColumnByTicketId(active.id);
    const overStatus = isStatus(over.id)
      ? over.id
      : findColumnByTicketId(over.id);

    if (!activeStatus || !overStatus || activeStatus === overStatus) return;

    setColumns((prev) =>
      prev.map((col) => {
        if (col.status === activeStatus) {
          return { ...col, tickets: col.tickets.filter((t) => t.id !== active.id) };
        }
        if (col.status === overStatus) {
          const ticket = findTicketById(active.id);
          if (!ticket) return col;
          return { ...col, tickets: [...col.tickets, { ...ticket, status: overStatus }] };
        }
        return col;
      })
    );
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveTicket(null);

    const targetStatus = over
      ? isStatus(over.id)
        ? over.id
        : findColumnByTicketId(over.id)
      : undefined;

    if (!targetStatus) {
      endDrag();
      return;
    }

    // The drag stays "in progress" until the move has been sent. Letting a
    // deferred refetch go first would read the board from before the move and
    // snap the card back to where it was dropped from.
    try {
      await api.tickets.move(active.id as string, targetStatus);
    } catch {
      // The optimistic columns are wrong now, so take the server's word.
      missedRefreshRef.current = true;
    }
    endDrag();
  };

  const handleTicketClick = (ticket: Ticket) => {
    openTicket(ticket);
  };

  const handleUpdate = async (id: string, data: TicketWrite) => {
    await api.tickets.update(id, data);
    loadBoard();
  };

  const handleDelete = async (id: string) => {
    await api.tickets.delete(id);
    loadBoard();
  };

  const handleCreate = async (data: TicketWrite) => {
    await api.tickets.create(data);
    setCreateForStatus(null);
    loadBoard();
  };

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex items-center justify-between px-6 h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Kanban</h1>
      </header>

      <FilterPanel
        state={filterState}
        tickets={loading ? null : allTickets}
        repos={repos}
        count={waiting || projectsErrored ? undefined : { shown: shownCount, total: projectCount }}
        onProjects={onProjects}
      />

      <div data-testid="board-scroll" className="flex-1 overflow-x-auto p-6">
        {waiting ? (
          <div className="flex items-center justify-center h-full text-slate-600">
            Loading board…
          </div>
        ) : projectsErrored ? (
          <ProjectsLoadError />
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCorners}
            onDragStart={handleDragStart}
            onDragOver={handleDragOver}
            onDragEnd={handleDragEnd}
            onDragCancel={() => {
              setActiveTicket(null);
              endDrag();
            }}
          >
            <div className="flex gap-6 h-full">
              {STATUSES.map((status) => (
                <Column
                  key={status}
                  status={status}
                  tickets={getColumnTickets(status)}
                  onTicketClick={handleTicketClick}
                  onAddTicket={setCreateForStatus}
                  addBlocked={addBlocked}
                />
              ))}
            </div>
            <DragOverlay>
              {activeTicket ? (
                <div className="w-80">
                  <TicketCard ticket={activeTicket} isDragging />
                </div>
              ) : null}
            </DragOverlay>
           </DndContext>
        )}
      </div>

      {createForStatus && (
        <CreateTicketModal
          projects={projects ?? []}
          filters={filters}
          defaultStatus={createForStatus}
          onClose={() => setCreateForStatus(null)}
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
            loadBoard();
          }}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
          onOpenTicket={switchTicket}
        />
      )}
    </div>
  );
}
