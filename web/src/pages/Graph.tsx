import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { Maximize, ZoomIn, ZoomOut } from "lucide-react";
import { api, type Ticket, type Project, type TicketWrite } from "../api/client";
import TicketEditor from "../components/TicketEditor";
import TicketCard from "../components/TicketCard";
import FilterPanel from "../components/FilterPanel";
import { useChangeGlow } from "../hooks/useChangeGlow";
import { useFilters } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { useTicketParam } from "../hooks/useTicketParam";
import { CHANGE_GLOW_CLASS } from "../lib/changeGlow";
import { awaitingProject } from "../lib/defaultProject";
import { inProject, matchesFilters, repoOptions } from "../lib/filters";
import {
  chainFinder,
  chainRole,
  computeGraphTopology,
  edgeChainRole,
  matchingBounds,
  positionGraph,
  type ChainRole,
  type EdgeChainRole,
  type PositionedColumn,
  type Size,
} from "../lib/graphLayout";
import {
  NO_HIGHLIGHT,
  forgetCard,
  highlightedCard,
  nextHighlight,
  type HighlightEvent,
  type HighlightState,
} from "../lib/graphHighlight";
import { MIN_COLUMN_GAP, laneCount, lanesHeight, planGutters, routeEdges } from "../lib/graphEdges";
import { mergeSizes } from "../lib/graphSizes";
import { columnHeading } from "../lib/graphText";
import { isDone } from "../lib/status";
import {
  FIT_PADDING,
  IDENTITY,
  canZoomIn,
  canZoomOut,
  fitTransform,
  isDrag,
  panBy,
  panIntoView,
  wheelPan,
  wheelZoomFactor,
  zoomAround,
  zoomIn,
  zoomOut,
  zoomPercent,
  type Extent,
  type Insets,
  type Point,
  type Transform,
} from "../lib/viewport";

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
const UPSTREAM_ARROW = "graph-arrow-upstream";
const DOWNSTREAM_ARROW = "graph-arrow-downstream";

// Hovering or focusing a card lights its chains and dims everything else:
// what blocks it in amber, what it blocks in blue, and cards and edges in a
// cycle with it, which are on both chains, in red, the colour back edges
// already use for cycles. The card itself gets a bright border of its own.
// The `*:` classes recolour the TicketCard's own border, and `!` keeps its
// hover border from winning. Full literal class strings, so Tailwind emits
// them.
const CARD_CHAIN_CLASSES: Record<ChainRole, string> = {
  focus: "*:border-slate-200! ring-2 ring-slate-200/40 shadow-lg shadow-black/40",
  upstream: "*:border-amber-500! ring-1 ring-amber-500/40 shadow-lg shadow-black/40",
  downstream: "*:border-blue-400! ring-1 ring-blue-400/35 shadow-lg shadow-black/40",
  cycle: "*:border-red-500! ring-1 ring-red-500/40 shadow-lg shadow-black/40",
  none: "opacity-22",
};

// A card or edge the filters don't match dims rather than leaving the graph,
// so the arrows stay whole. A lit chain overrides this while it's shown.
const FILTERED_OUT = "opacity-20";

const EDGE_CHAIN_STYLES: Record<Exclude<EdgeChainRole, "none">, { className: string; marker: string }> = {
  upstream: { className: "stroke-amber-500", marker: UPSTREAM_ARROW },
  downstream: { className: "stroke-blue-400", marker: DOWNSTREAM_ARROW },
  cycle: { className: "stroke-red-500", marker: BACK_ARROW },
};

const NO_SIZES: ReadonlyMap<string, Size> = new Map();

// Safari reports a trackpad pinch as gesture events, not as ctrl+wheel. The
// DOM typings don't include them.
interface GestureLike extends UIEvent {
  scale: number;
  clientX: number;
  clientY: number;
}

// A pointer that went down on the viewport, until it goes up. It pans only
// once it has moved DRAG_THRESHOLD pixels from where it went down.
interface PointerDrag {
  pointerId: number;
  start: Point;
  last: Point;
  panning: boolean;
}

// Fit keeps the bottom strip clear of the toolbar: its bottom-4 offset (16px),
// its height (34px) and 8px of air above it.
const FIT_INSETS: Insets = { top: FIT_PADDING, right: FIT_PADDING, bottom: 16 + 34 + 8, left: FIT_PADDING };

const TOOL_BUTTON =
  "flex items-center justify-center w-7 h-7 rounded text-slate-400 transition-colors hover:text-slate-200 hover:bg-slate-800 disabled:text-slate-700 disabled:hover:bg-transparent";

// With filters set, a column's count is its matching cards out of all of them.
function ColumnHeader({ column, matching }: { column: PositionedColumn; matching?: number }) {
  return (
    <div
      className="absolute top-0 flex items-baseline gap-2"
      style={{ left: column.x, width: column.width }}
    >
      <h3 className="text-xs font-medium text-slate-400">{columnHeading(column.index)}</h3>
      <span className="text-[11px] text-slate-600">
        {matching === undefined ? column.count : `${matching} of ${column.count}`}
      </span>
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
  // Null until the first load.
  const [projects, setProjects] = useState<Project[] | null>(null);
  // Every project's tickets, null until the first fetch. The graph is laid
  // out from the selected project's alone; the other filters only dim cards.
  // Neither refetches: the editor can open any project's ticket.
  const [fetched, setFetched] = useState<Ticket[] | null>(null);
  // Bumped after an edit to refetch.
  const [version, setVersion] = useState(0);
  const [sizes, setSizes] = useState(NO_SIZES);
  // What the cards' pointer and focus events have lit. It is not tied to the
  // topology it was picked on: a refetch leaves the cards where they are, so a
  // live update under a still pointer must not unlight the graph. Only a lit
  // card that has left the graph is dropped, below.
  const [picked, setPicked] = useState<HighlightState>(NO_HIGHLIGHT);
  // Pan and zoom: screen = translate + scale * canvas. `fitPending` is true
  // from first load until the fit has run.
  const [transform, setTransform] = useState<Transform>(IDENTITY);
  const [fitPending, setFitPending] = useState(true);
  const [viewportSize, setViewportSize] = useState<Extent | null>(null);
  const [panning, setPanning] = useState(false);
  const filterState = useFilters();
  const { filters } = filterState;

  // Reloaded with the tickets, so the editor names a project that was renamed
  // elsewhere. A failed reload keeps the projects already loaded, and a reply
  // to a fetch a newer one has overtaken is dropped rather than put on screen.
  // Only a failed first load settles on none.
  useEffect(() => {
    let cancelled = false;
    api.projects
      .list()
      .then((loaded) => {
        if (!cancelled) setProjects(loaded ?? []);
      })
      .catch(() => {
        if (!cancelled) setProjects((prev) => prev ?? []);
      });
    return () => {
      cancelled = true;
    };
  }, [version]);

  useEffect(() => {
    let cancelled = false;
    api.tickets
      .list()
      .then((tickets) => {
        if (!cancelled) setFetched(tickets || []);
      })
      .catch(() => {
        // A failed refetch keeps the graph that is on screen — an open editor
        // included. Only a failed first load settles on an empty graph.
        if (!cancelled) setFetched((prev) => prev ?? []);
      });
    return () => {
      cancelled = true;
    };
  }, [version]);

  const refresh = useCallback(() => setVersion((v) => v + 1), []);
  // The filter bar picks a project for a URL without one; until it has, the
  // graph waits rather than laying out every project's tickets.
  const loading = fetched === null || awaitingProject(filters.project, projects);
  // The open ticket comes from the URL, so ?ticket=KEY opens it on load.
  const {
    selected: selectedTicket,
    closeRequested,
    open: openSelected,
    close: closeSelected,
    cancelClose,
    onDirtyChange,
    url: ticketUrl,
  } = useTicketParam(fetched);

  // Pan, zoom and the lit chain all live outside `fetched`, so refetching
  // leaves them as they are; the open editor is named by the URL, so a refetch
  // only hands it the refreshed ticket.
  useLiveRefresh(refresh);

  // Only the selected project's tickets are laid out. A dependency on another
  // project's unfinished ticket is not an edge: the card counts it as a
  // hidden blocker and sits right of Ready, as for any blocker not drawn.
  const shownTickets = useMemo(() => inProject(fetched ?? [], filters.project), [fetched, filters.project]);
  // Once per fetched ticket set or project; measuring only repositions.
  const topology = useMemo(() => computeGraphTopology(shownTickets), [shownTickets]);
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
  // Cards a live refresh just changed or brought in glow for two seconds.
  // Only open tickets are watched, so a ticket that left for done is simply
  // gone from the next fetch and never flashes on its way out. Every
  // project's are watched, not only the cards on the graph, so switching
  // project brings no card in and nothing flashes.
  const openTickets = useMemo(() => fetched?.filter((t) => !isDone(t.status)) ?? null, [fetched]);
  const glowing = useChangeGlow(openTickets);
  // The ids of the cards the filters match, or null when no filter but the
  // project is set: every card on the graph is in the project.
  const matching = useMemo(
    () =>
      filterState.active
        ? new Set(topology.nodes.filter((node) => matchesFilters(node.ticket, filters)).map((node) => node.id))
        : null,
    [filterState.active, topology, filters],
  );
  const dimmed = (id: string) => matching !== null && !matching.has(id);
  // Matching cards per column, for the column headers.
  const matchingPerColumn = useMemo(() => {
    if (!matching) return null;
    const counts = topology.columnCounts.map(() => 0);
    for (const node of topology.nodes) if (matching.has(node.id)) counts[node.column]++;
    return counts;
  }, [matching, topology]);
  const repos = useMemo(() => repoOptions(shownTickets, filters.repo), [shownTickets, filters.repo]);
  // Adjacency once per topology; the chains once per pick, not per render.
  const findChains = useMemo(() => chainFinder(topology), [topology]);
  const lit = highlightedCard(picked);
  const chains = useMemo(() => (lit === null ? null : findChains(lit)), [lit, findChains]);
  // chainFinder answers null for an id that is not a node, which is how a
  // refetch that removed the lit card shows up here. Forgetting it is state
  // adjusted while rendering: React renders again before committing, so the
  // graph is never painted with a pick it no longer holds, and the card can't
  // light up again on its own if a later refetch brings it back.
  if (lit !== null && chains === null) setPicked((prev) => forgetCard(prev, lit));
  // graphHighlight.ts holds the rule; the cards below only report what they
  // see. A state it leaves unchanged is returned by identity, so pointermoves
  // over the card already lit re-render nothing.
  const onHighlight = (event: HighlightEvent) => setPicked((prev) => nextHighlight(prev, event));
  // Opening the panel clears the highlight, since the pointer and focus it
  // came from are about to move.
  const openTicket = (ticket: Ticket) => {
    onHighlight({ type: "editorOpened" });
    openSelected(ticket);
  };

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

  // The view fits itself only on first load and when the project changes,
  // which lays out a different graph. Other filters, tickets appearing or
  // leaving, the column count changing, cards resizing and the window
  // resizing never move it; the Fit button does. A pending fit waits for the
  // tickets to arrive and form a graph, for every card to be measured, so it
  // fits the real extent, and for the viewport's size. With no open tickets
  // the fit stays pending, so the first graph shown is fitted. The fit is
  // state adjusted while rendering: React renders again before committing, so
  // the unfitted graph is never painted, and the canvas stays invisible while
  // a fit is pending.
  const [laidOutProject, setLaidOutProject] = useState(filters.project.toLowerCase());
  if (filters.project.toLowerCase() !== laidOutProject) {
    setLaidOutProject(filters.project.toLowerCase());
    setFitPending(true);
  }
  const measured = useMemo(() => topology.nodes.every((node) => sizes.has(node.id)), [topology, sizes]);
  const hasGraph = !loading && topology.nodes.length > 0;
  if (fitPending && hasGraph && measured && viewportSize) {
    setFitPending(false);
    setTransform(fitTransform({ width: canvasWidth, height: canvasHeight }, viewportSize, FIT_INSETS));
  }

  // The viewport's size, for fit and for zooming around its centre. Resizing
  // the window keeps the transform. Wheel and Safari gesture listeners are
  // added here rather than as React props, because they have to be
  // non-passive to stop the browser scrolling or zooming the page.
  const attachViewport = useCallback((el: HTMLDivElement | null) => {
    if (!el) return;
    const sizeObserver = new ResizeObserver(() => {
      const width = el.clientWidth;
      const height = el.clientHeight;
      setViewportSize((prev) => (prev && prev.width === width && prev.height === height ? prev : { width, height }));
    });
    sizeObserver.observe(el);

    const showsGraph = () => el.querySelector("[data-graph-canvas]") !== null;
    // Ctrl or meta+wheel and pinches are always kept from zooming the page,
    // including while the viewport shows the loading or empty state.
    const pointIn = (clientX: number, clientY: number): Point => {
      const rect = el.getBoundingClientRect();
      return { x: clientX - rect.left, y: clientY - rect.top };
    };
    // Plain wheel pans; ctrl or meta zooms around the cursor, which is also
    // how Chrome and Firefox report a trackpad pinch.
    const onWheel = (e: WheelEvent) => {
      const zoom = e.ctrlKey || e.metaKey;
      if (zoom) e.preventDefault();
      if (!showsGraph()) return;
      e.preventDefault();
      if (zoom) {
        const at = pointIn(e.clientX, e.clientY);
        const factor = wheelZoomFactor(e.deltaY, e.deltaMode);
        setTransform((t) => zoomAround(t, at, factor));
      } else {
        const by = wheelPan(e);
        setTransform((t) => panBy(t, by.x, by.y));
      }
    };
    // Safari's gesture scale is cumulative from gesturestart.
    let gestureScale = 1;
    const onGestureStart = (e: Event) => {
      e.preventDefault();
      gestureScale = 1;
    };
    const onGestureChange = (e: Event) => {
      e.preventDefault();
      if (!showsGraph()) return;
      const gesture = e as GestureLike;
      if (!(gesture.scale > 0)) return;
      const factor = gesture.scale / gestureScale;
      gestureScale = gesture.scale;
      const at = pointIn(gesture.clientX, gesture.clientY);
      setTransform((t) => zoomAround(t, at, factor));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    el.addEventListener("gesturestart", onGestureStart, { passive: false });
    el.addEventListener("gesturechange", onGestureChange, { passive: false });
    el.addEventListener("gestureend", onGestureStart, { passive: false });
    return () => {
      sizeObserver.disconnect();
      el.removeEventListener("wheel", onWheel);
      el.removeEventListener("gesturestart", onGestureStart);
      el.removeEventListener("gesturechange", onGestureChange);
      el.removeEventListener("gestureend", onGestureStart);
    };
  }, []);

  // Dragging pans, from empty space or from a card. A pointer that moves no
  // more than DRAG_THRESHOLD before it goes up is a click, and a card opens
  // its panel; one that moves further pans and swallows the click that
  // follows. The pointer is only captured once it pans, so hovering cards is
  // untouched.
  const dragRef = useRef<PointerDrag | null>(null);
  const swallowClickRef = useRef(false);

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    swallowClickRef.current = false;
    if (!hasGraph || e.button !== 0 || !e.isPrimary) return;
    if ((e.target as Element).closest("[data-graph-toolbar]")) return;
    const at = { x: e.clientX, y: e.clientY };
    dragRef.current = { pointerId: e.pointerId, start: at, last: at, panning: false };
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    if (e.pointerType === "mouse" && (e.buttons & 1) === 0) {
      // The left button went up without a pointerup: outside the window
      // before the drag started, or while another button is still held.
      endDrag(e.currentTarget, e.pointerId);
      return;
    }
    const at = { x: e.clientX, y: e.clientY };
    if (!drag.panning) {
      if (!isDrag(drag.start, at)) return;
      drag.panning = true;
      e.currentTarget.setPointerCapture(e.pointerId);
      setPanning(true);
    }
    const dx = at.x - drag.last.x;
    const dy = at.y - drag.last.y;
    drag.last = at;
    setTransform((t) => panBy(t, dx, dy));
  };

  // Forgets the drag without swallowing a click, and lets go of the pointer.
  const endDrag = (el: HTMLDivElement, pointerId: number) => {
    dragRef.current = null;
    setPanning(false);
    if (el.hasPointerCapture(pointerId)) el.releasePointerCapture(pointerId);
  };

  // Capture can also be lost without a pointerup reaching the viewport.
  const handleLostPointerCapture = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragRef.current?.pointerId !== e.pointerId) return;
    dragRef.current = null;
    setPanning(false);
  };

  // No context menu over a pan in progress, e.g. a right click mid-drag.
  const handleContextMenu = (e: React.MouseEvent<HTMLDivElement>) => {
    if (dragRef.current?.panning) e.preventDefault();
  };

  const handlePointerEnd = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    dragRef.current = null;
    if (!drag.panning) return;
    setPanning(false);
    if (e.type === "pointerup") swallowClickRef.current = true;
  };

  const handleClickCapture = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!swallowClickRef.current) return;
    swallowClickRef.current = false;
    e.stopPropagation();
    e.preventDefault();
  };

  // A card reached with Tab is panned into view. Pointer focus is left alone,
  // so clicking a card near the edge doesn't move the graph under the pointer.
  const handleFocus = (e: React.FocusEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement;
    if (!target.dataset.ticketId || !target.matches(":focus-visible")) return;
    const by = panIntoView(target.getBoundingClientRect(), e.currentTarget.getBoundingClientRect());
    if (by.x !== 0 || by.y !== 0) setTransform((t) => panBy(t, by.x, by.y));
  };

  const centre: Point = { x: (viewportSize?.width ?? 0) / 2, y: (viewportSize?.height ?? 0) / 2 };
  // Fit frames the cards the filters match; the dimmed ones keep their places
  // and may end up outside the view, and so may a back edge's lane or a
  // self-loop between two matching cards, which run outside their box. With no
  // filter set, or with every card or none of them matching, matchingBounds
  // answers null and Fit frames the whole canvas, as first load always does.
  const fit = () => {
    if (!viewportSize) return;
    const box = matchingBounds(layout.nodes, matching) ?? { width: canvasWidth, height: canvasHeight };
    setTransform(fitTransform(box, viewportSize, FIT_INSETS));
  };

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex items-center justify-between px-6 h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Dependencies</h1>
      </header>

      <FilterPanel
        state={filterState}
        tickets={fetched}
        repos={repos}
        count={loading ? undefined : { shown: matching?.size ?? topology.nodes.length, total: topology.nodes.length }}
      />

      <div
        ref={attachViewport}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerEnd}
        onPointerCancel={handlePointerEnd}
        onLostPointerCapture={handleLostPointerCapture}
        onContextMenu={handleContextMenu}
        onClickCapture={handleClickCapture}
        onFocus={handleFocus}
        className={`relative flex-1 min-h-0 overflow-clip ${
          hasGraph ? `select-none touch-none ${panning ? "cursor-grabbing" : "cursor-grab"}` : ""
        }`}
      >
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
            data-graph-canvas
            className={`relative origin-top-left ${fitPending ? "invisible" : ""}`}
            style={{
              width: canvasWidth,
              height: canvasHeight,
              transform: `translate(${transform.x}px, ${transform.y}px) scale(${transform.k})`,
            }}
            // Each card clears its own highlight as the pointer leaves it, so
            // this is only a safety net for a pointer that leaves the graph
            // without the card under it seeing its own leave. Focus moving
            // out of the graph altogether clears the focus ring's highlight;
            // moving from card to card is left to the cards.
            onPointerLeave={() => onHighlight({ type: "pointerLeftGraph" })}
            onBlur={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget)) onHighlight({ type: "focusLeftGraph" });
            }}
          >
            {layout.columns.map((column) => (
              <ColumnHeader
                key={column.index}
                column={column}
                matching={matchingPerColumn?.[column.index]}
              />
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
                <Arrowhead id={UPSTREAM_ARROW} className="fill-amber-500" />
                <Arrowhead id={DOWNSTREAM_ARROW} className="fill-blue-400" />
              </defs>
              {edges.map((edge) => {
                const role = chains ? edgeChainRole(chains, edge) : "none";
                const lit = role === "none" ? null : EDGE_CHAIN_STYLES[role];
                const stroke = lit?.className ?? (edge.back ? "stroke-red-500" : "stroke-slate-600");
                const marker = lit?.marker ?? (edge.back ? BACK_ARROW : ARROW);
                const faded = chains ? !lit : dimmed(edge.from) || dimmed(edge.to);
                return (
                  <path
                    key={`${edge.from}->${edge.to}`}
                    d={edge.d}
                    className={`fill-none transition-opacity duration-150 ${stroke} ${
                      faded ? (chains ? "opacity-12" : FILTERED_OUT) : ""
                    }`}
                    strokeWidth={lit ? 2 : 1.5}
                    markerEnd={`url(#${marker})`}
                  />
                );
              })}
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
                  openTicket(node.ticket);
                }}
                // Move as well as enter: after a re-render under a still
                // pointer (the panel closing, a refetch) React sends no enter
                // for the card already under it, and the rule ignores a move
                // over the card already lit.
                onPointerEnter={() => onHighlight({ type: "pointerOverCard", id: node.id })}
                onPointerMove={() => onHighlight({ type: "pointerOverCard", id: node.id })}
                onPointerLeave={() => onHighlight({ type: "pointerLeftCard", id: node.id })}
                // A card the pointer pressed on is focused without a ring,
                // and must not hold the graph dimmed once the pointer leaves.
                onFocus={(e) =>
                  onHighlight({
                    type: "cardFocused",
                    id: node.id,
                    focusVisible: e.currentTarget.matches(":focus-visible"),
                  })
                }
                onBlur={() => onHighlight({ type: "cardBlurred", id: node.id })}
                className={`absolute w-64 rounded-lg transition-[opacity,box-shadow] duration-150 ${
                  chains ? CARD_CHAIN_CLASSES[chainRole(chains, node.id)] : dimmed(node.id) ? FILTERED_OUT : ""
                } ${glowing.has(node.id) ? CHANGE_GLOW_CLASS : ""}`}
                style={{ left: node.x, top: node.y }}
              >
                <TicketCard
                  ticket={node.ticket}
                  graph={node}
                  onClick={() => openTicket(node.ticket)}
                />
              </div>
            ))}
          </div>
        )}

        {hasGraph && (
          <div
            data-graph-toolbar
            className="absolute bottom-4 right-4 z-10 flex items-center gap-0.5 rounded-md border border-slate-700 bg-slate-900 p-0.5 shadow-lg shadow-black/40 cursor-default"
          >
            <button type="button" onClick={fit} title="Fit to screen" aria-label="Fit to screen" className={TOOL_BUTTON}>
              <Maximize className="w-4 h-4" />
            </button>
            <div className="w-px h-4 mx-0.5 bg-slate-700" />
            <button
              type="button"
              onClick={() => setTransform((t) => zoomOut(t, centre))}
              disabled={!canZoomOut(transform)}
              title="Zoom out"
              aria-label="Zoom out"
              className={TOOL_BUTTON}
            >
              <ZoomOut className="w-4 h-4" />
            </button>
            <span className="w-11 text-center text-xs tabular-nums text-slate-400" aria-label="Zoom level">
              {zoomPercent(transform.k)}
            </span>
            <button
              type="button"
              onClick={() => setTransform((t) => zoomIn(t, centre))}
              disabled={!canZoomIn(transform)}
              title="Zoom in"
              aria-label="Zoom in"
              className={TOOL_BUTTON}
            >
              <ZoomIn className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>

      {selectedTicket && (
        <TicketEditor
          ticket={selectedTicket}
          projects={projects ?? []}
          ticketUrl={ticketUrl}
          closeRequested={closeRequested}
          onCloseCancelled={cancelClose}
          onDirtyChange={onDirtyChange}
          onClose={() => {
            closeSelected();
            refresh();
          }}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
        />
      )}
    </div>
  );
}
