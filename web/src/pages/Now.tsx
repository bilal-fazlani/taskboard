import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router-dom";
import { api, type LandedTicket, type Now as NowData, type NowTicket, type Project } from "../api/client";
import { fieldClass } from "../components/controlStyles";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { activeProjects, projectLabel } from "../lib/defaultProject";
import {
  TICK_MS,
  editorLink,
  landedAgo,
  reviewNote,
  runningFor,
  shortSha,
  withinLandedWindow,
} from "../lib/now";
import { readNowProject, rememberNowProject, restoredNowProject } from "../lib/nowProject";
import { AGENT_REVIEW_STATUS, IN_PROGRESS_STATUS, STATUS_LABELS, STATUS_STYLES } from "../lib/status";

// The Now page: what is moving right now, and what just landed, across every
// project unless the dropdown narrows it to one. The project lives in the URL
// (`project`, a prefix) like the views' filter, and Now remembers its own
// pick, apart from the views' last project (see nowProject.ts): a URL without
// a project gets the pick last made here back, "All projects" included. A
// remembered project archived or deleted since, like nothing remembered,
// gives "All projects". A URL naming a project wins and is remembered. It is
// the present only; the history of status changes is the Activity feed's.

/** The `project` parameter, or "" for every project. */
const PROJECT_PARAM = "project";

/**
 * One arrival at the page, a location: the pick remembered then, and whether
 * it has been checked against the projects yet. Once checked, `pick` is the
 * project to restore into the URL, or "" for "All projects".
 */
interface Arrival {
  key: string;
  pick: string;
  checked: boolean;
}

/**
 * Reads the remembered pick for an arrival at `project` (the URL's, "" for
 * none). Storage is read here only, once per location, so a pick another tab
 * saves never moves a page already open; the sidebar's Now link is a new
 * location, and restores. It is checked at once when the projects are in
 * hand, or have failed to load; else when they first answer.
 */
function arrive(key: string, project: string, projects: readonly Project[] | null, projectsFailed: boolean): Arrival {
  const remembered = project === "" ? readNowProject() : "";
  if (remembered === "") return { key, pick: "", checked: true };
  if (projects !== null) return { key, pick: restoredNowProject(projects, remembered), checked: true };
  if (projectsFailed) return { key, pick: "", checked: true };
  return { key, pick: remembered, checked: false };
}

/** The clock the timers read, redrawn every TICK_MS. */
function useClock(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(timer);
  }, []);
  return now;
}

function Chip({ prefix }: { prefix: string }) {
  if (!prefix) return null;
  return (
    <span data-testid="project-chip" className="shrink-0 rounded border border-slate-700 px-1.5 text-[11px] text-slate-400">
      {prefix}
    </span>
  );
}

function TicketCard({ ticket, now }: { ticket: NowTicket; now: number }) {
  const review = ticket.status === AGENT_REVIEW_STATUS;
  const note = reviewNote(ticket);
  const pct = ticket.subtasksTotal > 0 ? Math.round((ticket.subtasksDone / ticket.subtasksTotal) * 100) : 0;
  return (
    <Link
      to={editorLink(ticket)}
      data-testid="now-card"
      className="flex flex-col gap-2 rounded-lg border border-slate-800 bg-slate-900 px-3 py-2.5 transition-colors hover:border-slate-700 hover:bg-slate-800/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500"
    >
      <div className="flex min-w-0 items-center gap-2">
        <span className="shrink-0 font-mono text-xs text-slate-500">{ticket.key}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{ticket.title}</span>
        <Chip prefix={ticket.projectPrefix} />
      </div>
      <div className="flex items-center gap-3 text-[11.5px] tabular-nums text-slate-500">
        <div
          role="progressbar"
          aria-label="Subtasks done"
          aria-valuemin={0}
          aria-valuemax={ticket.subtasksTotal}
          aria-valuenow={ticket.subtasksDone}
          className="h-1 flex-1 overflow-hidden rounded-sm bg-slate-800"
        >
          <i
            className={`block h-full transition-[width] duration-500 ease-out ${review ? "bg-violet-400" : "bg-blue-400"}`}
            style={{ width: `${pct}%` }}
          />
        </div>
        <span data-testid="subtasks">
          {ticket.subtasksDone}/{ticket.subtasksTotal}
        </span>
        {note && (
          <span data-testid="review-note" className={note.tone === "approved" ? "text-green-400" : "text-violet-400"}>
            {note.text}
          </span>
        )}
        <span data-testid="running-for" title={`Since ${new Date(ticket.since).toLocaleString()}`}>
          {runningFor(ticket.since, now)}
        </span>
      </div>
    </Link>
  );
}

function Column({
  status,
  tickets,
  now,
  empty,
}: {
  status: typeof IN_PROGRESS_STATUS | typeof AGENT_REVIEW_STATUS;
  tickets: NowTicket[];
  now: number;
  empty: string;
}) {
  const id = `now-${status}`;
  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col gap-2">
      <h2 id={id} className="flex items-center gap-2 text-xs font-medium text-slate-400">
        <span className={`rounded-full px-2 py-px text-[11px] font-medium ${STATUS_STYLES[status]}`}>
          {STATUS_LABELS[status]}
        </span>
        <span className="font-mono text-slate-500">{tickets.length}</span>
      </h2>
      {tickets.length === 0 ? (
        <p className="rounded-lg border border-dashed border-slate-800 px-3 py-4 text-center text-xs text-slate-500">
          {empty}
        </p>
      ) : (
        tickets.map((t) => <TicketCard key={t.id} ticket={t} now={now} />)
      )}
    </section>
  );
}

function LandedRow({ ticket, now }: { ticket: LandedTicket; now: number }) {
  return (
    <Link
      to={editorLink(ticket)}
      data-testid="landed-row"
      className="grid grid-cols-[72px_minmax(0,1fr)_auto_auto_76px] items-center gap-3 border-t border-slate-800 px-3 py-2 text-sm transition-colors first:border-t-0 hover:bg-slate-800/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-blue-500"
    >
      <span className="font-mono text-xs text-slate-500">{ticket.key}</span>
      <span className="truncate text-slate-200">{ticket.title}</span>
      <Chip prefix={ticket.projectPrefix} />
      <span className="flex gap-1">
        {ticket.commits.map((c) => (
          <span
            key={`${c.repo}@${c.sha}`}
            data-testid="sha"
            title={`${c.repo}@${c.sha}`}
            className="rounded bg-slate-800 px-1.5 py-px font-mono text-[11px] text-slate-400"
          >
            {shortSha(c.sha)}
          </span>
        ))}
      </span>
      <span data-testid="landed-ago" className="text-right text-[11.5px] tabular-nums text-slate-500">
        {landedAgo(ticket.doneAt, now)}
      </span>
    </Link>
  );
}

export default function Now() {
  const [params, setParams] = useSearchParams();
  const project = params.get(PROJECT_PARAM) ?? "";
  const [data, setData] = useState<NowData | null>(null);
  const [failed, setFailed] = useState(false);
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectsFailed, setProjectsFailed] = useState(false);
  const now = useClock();
  // Only the latest request's answer is shown, so a slow answer for the
  // project picked before never overwrites the one picked now.
  const latest = useRef(0);

  // A URL without a project gets the remembered pick back once the projects
  // have loaded and show it is still active; until then nothing loads, so
  // the page never shows every project on its way to one. A remembered
  // project archived or deleted since, or projects that fail to load, leave
  // "All projects". The pick is read once per arrival (see arrive), so a
  // redraw or a live refresh never moves a page already open.
  const { key: locationKey } = useLocation();
  const [arrival, setArrival] = useState(() => arrive(locationKey, project, null, false));
  let current = arrival;
  if (arrival.key !== locationKey) {
    current = arrive(locationKey, project, projects, projectsFailed);
    setArrival(current);
  }
  const restore = project === "" && current.checked ? current.pick : "";
  const holding = project === "" && current.pick !== "";

  useEffect(() => {
    if (!restore) return;
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.set(PROJECT_PARAM, restore);
        return next;
      },
      { replace: true },
    );
  }, [restore, setParams]);

  // A project the URL names wins, and is what Now shows next time.
  useEffect(() => {
    if (project) rememberNowProject(project);
  }, [project]);

  const load = useCallback(() => {
    if (holding) return;
    const request = ++latest.current;
    api.now.get(project || undefined).then(
      (next) => {
        if (request !== latest.current) return;
        setData(next);
        setFailed(false);
      },
      // A failed refetch keeps what is on screen; the next change retries.
      () => request === latest.current && setFailed(true),
    );
  }, [project, holding]);

  // A failed load keeps "All projects" and whatever the URL names. The first
  // answer after an arrival settles its remembered pick, once.
  const loadProjects = useCallback(() => {
    api.projects.list().then(
      (list) => {
        setProjects(list);
        setProjectsFailed(false);
        setArrival((a) => (a.checked ? a : { ...a, pick: restoredNowProject(list, a.pick), checked: true }));
      },
      () => {
        setProjectsFailed(true);
        setArrival((a) => (a.checked ? a : { ...a, pick: "", checked: true }));
      },
    );
  }, []);

  useEffect(() => {
    load();
  }, [load]);
  useEffect(() => {
    loadProjects();
  }, [loadProjects]);

  // Cards move between the groups, and timers restart, as agents move
  // tickets: every change anywhere refetches, in one request.
  useLiveRefresh(() => {
    load();
    loadProjects();
  });

  // Active projects by name, plus the one the URL names when it is archived
  // or unknown, so the dropdown always shows what the page is narrowed to.
  const options = useMemo(() => {
    const list = (projects ? activeProjects(projects) : []).map((p) => ({ value: p.prefix, label: projectLabel(p) }));
    if (project && !list.some((o) => o.value.toLowerCase() === project.toLowerCase())) {
      const named = projects?.find((p) => p.prefix.toLowerCase() === project.toLowerCase());
      list.push({ value: named?.prefix ?? project, label: named ? projectLabel(named) : project });
    }
    return list;
  }, [projects, project]);
  const selected = options.find((o) => o.value.toLowerCase() === project.toLowerCase())?.value ?? "";

  // An idle board fetches nothing new, so each tick drops what has turned a
  // day old since the last fetch.
  const landed = useMemo(() => (data ? withinLandedWindow(data.landed, now) : []), [data, now]);

  // "All projects" leaves the URL without a project, so it is remembered
  // here rather than by the effect above.
  const pick = (value: string) => {
    rememberNowProject(value);
    const next = new URLSearchParams(params);
    if (value) next.set(PROJECT_PARAM, value);
    else next.delete(PROJECT_PARAM);
    setParams(next);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-slate-800 px-6">
        <h1 className="text-lg font-semibold text-white">Now</h1>
        <select
          aria-label="Project"
          value={selected}
          onChange={(e) => pick(e.target.value)}
          className={fieldClass(selected !== "")}
        >
          <option value="">All projects</option>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <span className="flex-1" />
        {failed ? (
          <span className="text-xs text-amber-400">Can't reach the board; retrying on the next change</span>
        ) : (
          <span className="flex items-center gap-1.5 text-[11px] text-green-400">
            <span className="h-1.5 w-1.5 rounded-full bg-green-400 shadow-[0_0_0_3px_rgba(74,222,128,0.15)] motion-safe:animate-pulse" />
            Live
          </span>
        )}
      </header>

      {data === null ? (
        <p className="px-6 py-8 text-sm text-slate-500">{failed ? "Could not load what is running." : "Loading…"}</p>
      ) : (
        <div className="flex flex-col gap-6 p-6">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Column status={IN_PROGRESS_STATUS} tickets={data.inProgress} now={now} empty="Nothing in progress" />
            <Column status={AGENT_REVIEW_STATUS} tickets={data.inReview} now={now} empty="Nothing in review" />
          </div>
          <section aria-labelledby="now-landed" className="flex flex-col gap-2">
            <h2 id="now-landed" className="flex items-center gap-2 text-xs font-medium text-slate-400">
              <span className={`rounded-full px-2 py-px text-[11px] font-medium ${STATUS_STYLES.done}`}>Landed</span>
              <span className="font-mono text-slate-500">last 24 hours</span>
            </h2>
            {landed.length === 0 ? (
              <p className="rounded-lg border border-dashed border-slate-800 px-3 py-4 text-center text-xs text-slate-500">
                Nothing landed in the last 24 hours
              </p>
            ) : (
              <div className="rounded-lg border border-slate-800 bg-slate-900">
                {landed.map((t) => (
                  <LandedRow key={t.id} ticket={t} now={now} />
                ))}
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
