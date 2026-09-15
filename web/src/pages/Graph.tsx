import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { api, type Ticket, type Project, type TicketWrite } from "../api/client";
import TicketPanel from "../components/TicketPanel";
import TicketCard from "../components/TicketCard";
import {
  computeGraphTopology,
  positionGraph,
  type PositionedColumn,
  type Size,
} from "../lib/graphLayout";
import { MIN_COLUMN_GAP, laneCount, lanesHeight, planGutters, routeEdges } from "../lib/graphEdges";
import { mergeSizes } from "../lib/graphSizes";
import { columnHeading } from "../lib/graphText";

// Cards are w-64. Heights are measured; this one is only the placeholder size
// of the empty Ready column and of a card in the moment before it's measured.
const CARD_SIZE: Size = { width: 256, height: 96 };
const ROW_GAP = 16;
// Where the first card starts when there are no back edges: room for the
// column headers above. Each back edge adds a lane between the headers and
// the cards.
const CARDS_TOP = 52;

const ARROW = "graph-arrow";
const BACK_ARROW = "graph-arrow-back";

const NO_SIZES: ReadonlyMap<string, Size> = new Map();

function ColumnHeader({ column }: { column: PositionedColumn }) {
  return (
    <div
      className="absolute top-0 flex items-baseline gap-2"
      style={{ left: column.x, width: column.width }}
    >
      <h3 className="text-xs font-medium text-slate-400">{columnHeading(column.index)}</h3>
      <span className="text-[11px] text-slate-600">{column.count}</span>
    </div>
  );
}

function Arrowhead({ id, className }: { id: string; className: string }) {
  return (
    <marker id={id} viewBox="0 0 10 10" refX="10" refY="5" markerWidth="7" markerHeight="7" orient="auto">
      <path d="M0 0L10 5L0 10z" className={className} />
    </marker>
  );
}

export default function Graph() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");
  // The tickets and the project selection they were fetched for. The page is
  // loading while they belong to a different project than the selected one.
  const [fetched, setFetched] = useState<{ projectId: string; tickets: Ticket[] } | null>(null);
  // Bumped after an edit to refetch the same project selection.
  const [version, setVersion] = useState(0);
  const [selectedTicket, setSelectedTicket] = useState<Ticket | null>(null);
  const [sizes, setSizes] = useState(NO_SIZES);

  useEffect(() => {
    api.projects.list().then(setProjects).catch(() => setProjects([]));
  }, []);

  useEffect(() => {
    let cancelled = false;
    api.tickets
      .list({ projectId: selectedProject || undefined })
      .then((tickets) => tickets || [])
      .catch((): Ticket[] => [])
      .then((tickets) => {
        if (!cancelled) setFetched({ projectId: selectedProject, tickets });
      });
    return () => {
      cancelled = true;
    };
  }, [selectedProject, version]);

  const refresh = () => setVersion((v) => v + 1);
  const loading = fetched === null || fetched.projectId !== selectedProject;

  // Once per fetched ticket set; measuring only repositions.
  const topology = useMemo(() => computeGraphTopology(fetched?.tickets ?? []), [fetched]);
  // Back edges' vertical runs need room in the gaps and beside the outer
  // columns, which depends only on the topology. positionGraph takes one gap
  // for every column, so every gap gets the widest one any gap needs.
  const gutters = useMemo(() => {
    const plan = planGutters(topology);
    return {
      columnGap: Math.max(MIN_COLUMN_GAP, ...plan.gaps),
      right: plan.right,
      origin: { x: plan.left, y: CARDS_TOP + lanesHeight(laneCount(topology)) },
    };
  }, [topology]);
  const { origin, columnGap } = gutters;
  const layout = useMemo(
    () =>
      positionGraph(topology, {
        sizes,
        defaultSize: CARD_SIZE,
        columnGap: gutters.columnGap,
        rowGap: ROW_GAP,
        origin: gutters.origin,
      }),
    [topology, sizes, gutters],
  );
  const edges = useMemo(() => routeEdges(layout), [layout]);

  // One ResizeObserver watches every card wrapper through the stable ref
  // callback below. Observing reports a card's size at the next rendering
  // step, after layout and before paint, and flushSync renders the positioned
  // graph in that same step, so the unmeasured stack is never painted.
  // mergeSizes hands back the same map when nothing changed, so the update
  // bails out; moving a card changes no card's size, so measuring can't loop.
  //
  // flushSync also flushes pending effects, and in development StrictMode
  // replays ref callbacks there: detach, then attach again, inside the
  // observer's own callback. Observing an element afresh at that point makes
  // the browser report "ResizeObserver loop completed with undelivered
  // notifications". So an element is observed once for as long as its ref
  // stays attached: attaching skips one already watched, and detaching only
  // unobserves in a microtask, once it's clear the ref wasn't attached again.
  const observerRef = useRef<ResizeObserver | null>(null);
  const attachedRef = useRef(new Set<Element>());
  const watchedRef = useRef(new Set<Element>());
  const observeCard = useCallback((el: HTMLDivElement | null) => {
    if (!el) return;
    if (!observerRef.current) {
      observerRef.current = new ResizeObserver((entries) => {
        const measured: [string, Size][] = [];
        for (const entry of entries) {
          const card = entry.target as HTMLElement;
          const id = card.dataset.ticketId;
          if (id) measured.push([id, { width: card.offsetWidth, height: card.offsetHeight }]);
        }
        flushSync(() => setSizes((prev) => mergeSizes(prev, measured)));
      });
    }
    const observer = observerRef.current;
    const attached = attachedRef.current;
    const watched = watchedRef.current;
    attached.add(el);
    if (!watched.has(el)) {
      watched.add(el);
      observer.observe(el);
    }
    return () => {
      attached.delete(el);
      queueMicrotask(() => {
        if (attached.has(el) || !watched.has(el)) return;
        watched.delete(el);
        observer.unobserve(el);
      });
    };
  }, []);

  const handleUpdate = async (id: string, data: TicketWrite) => {
    await api.tickets.update(id, data);
    refresh();
  };

  const handleDelete = async (id: string) => {
    await api.tickets.delete(id);
    refresh();
  };

  const ready = layout.columns[0];
  // Room right of the last column for back edges' vertical runs and
  // self-loops. An empty Ready column still shows its placeholder, which may
  // be taller than every stacked column.
  const canvasWidth = layout.width + gutters.right;
  const canvasHeight = Math.max(layout.height, origin.y + CARD_SIZE.height);

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex items-center justify-between px-6 h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Graph</h1>
        <select
          value={selectedProject}
          onChange={(e) => setSelectedProject(e.target.value)}
          className="bg-slate-800 text-sm text-slate-300 rounded-md border border-slate-700 px-3 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500"
        >
          <option value="">All Projects</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.icon} {p.name}
            </option>
          ))}
        </select>
      </header>

      <div className="flex-1 overflow-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center h-full text-slate-600">
            Loading graph…
          </div>
        ) : topology.nodes.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-1 h-full text-center">
            <p className="text-sm text-slate-400">No open tickets</p>
            <p className="text-xs text-slate-600">
              Tickets that aren't done show up here, arranged by what blocks them.
            </p>
          </div>
        ) : (
          <div
            className="relative"
            style={{ width: canvasWidth, height: canvasHeight }}
          >
            {layout.columns.map((column) => (
              <ColumnHeader key={column.index} column={column} />
            ))}
            {layout.columns.slice(1).map((column) => (
              <div
                key={column.index}
                className="absolute top-0 border-l border-dashed border-slate-800"
                style={{ left: column.x - columnGap / 2, height: canvasHeight }}
              />
            ))}
            {ready && ready.count === 0 && (
              <div
                className="absolute flex items-center justify-center text-xs text-slate-700 border border-dashed border-slate-800 rounded-lg"
                style={{ left: ready.x, top: origin.y, width: ready.width, height: CARD_SIZE.height }}
              >
                Nothing ready to start
              </div>
            )}

            <svg
              className="absolute inset-0 overflow-visible pointer-events-none"
              width={canvasWidth}
              height={canvasHeight}
              aria-hidden="true"
            >
              <defs>
                <Arrowhead id={ARROW} className="fill-slate-600" />
                <Arrowhead id={BACK_ARROW} className="fill-red-500" />
              </defs>
              {edges.map((edge) => (
                <path
                  key={`${edge.from}->${edge.to}`}
                  d={edge.d}
                  className={`fill-none ${edge.back ? "stroke-red-500" : "stroke-slate-600"}`}
                  strokeWidth={1.5}
                  markerEnd={`url(#${edge.back ? BACK_ARROW : ARROW})`}
                />
              ))}
            </svg>

            {layout.nodes.map((node) => (
              <div
                key={node.id}
                ref={observeCard}
                data-ticket-id={node.id}
                role="button"
                tabIndex={0}
                aria-label={`${node.ticket.projectPrefix}-${node.ticket.number} ${node.ticket.title}`}
                onKeyDown={(e) => {
                  if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return;
                  e.preventDefault();
                  setSelectedTicket(node.ticket);
                }}
                className="absolute w-64 rounded-lg"
                style={{ left: node.x, top: node.y }}
              >
                <TicketCard
                  ticket={node.ticket}
                  graph={node}
                  onClick={() => setSelectedTicket(node.ticket)}
                />
              </div>
            ))}
          </div>
        )}
      </div>

      {selectedTicket && (
        <TicketPanel
          ticket={selectedTicket}
          projects={projects}
          onClose={() => {
            setSelectedTicket(null);
            refresh();
          }}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
        />
      )}
    </div>
  );
}
