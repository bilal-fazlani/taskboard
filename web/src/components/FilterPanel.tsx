import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { Layers, Search, X } from "lucide-react";
import { api, type Epic, type Label, type Project } from "../api/client";
import type { FilterState } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { CONTROL_BUTTON, fieldClass, segmentClass, segmentedClass } from "./controlStyles";
import FilterMultiSelect from "./FilterMultiSelect";
import { PriorityIcon } from "./PriorityBadge";
import ProjectSelect from "./ProjectSelect";
import { activeProjects, namedProject, type ActivityTicket } from "../lib/defaultProject";
import { NO_EPIC, urlValue, type MultiFilterKey, type SelectOption, type UnmatchedMode } from "../lib/filters";
import { PRIORITIES } from "../lib/priority";
import { staleFilters } from "../lib/staleFilters";
import { STATUSES, STATUS_COLORS, STATUS_LABELS, isStatus } from "../lib/status";

const STATUS_OPTIONS: SelectOption[] = STATUSES.map((s) => ({ value: s, label: STATUS_LABELS[s] }));
const PRIORITY_OPTIONS: SelectOption[] = PRIORITIES.map((p) => ({ value: p, label: p.charAt(0).toUpperCase() + p.slice(1) }));

// How each list draws its options, as the rest of the app draws them: the
// status's column dot, the priority's icon, the epic's icon ("No epic" plain),
// the label's colour chip and the repo in monospace. A value the URL names
// that isn't offered draws as plain text.
const plain = (o: SelectOption) => <span className="truncate">{o.label}</span>;
const drawStatus = (o: SelectOption) => (
  <>
    <span className={`h-2 w-2 shrink-0 rounded-full ${isStatus(o.value) ? STATUS_COLORS[o.value] : "bg-transparent"}`} />
    {plain(o)}
  </>
);
const drawPriority = (o: SelectOption) => (
  <>
    <PriorityIcon priority={o.value} />
    {plain(o)}
  </>
);
const drawEpic = (o: SelectOption) =>
  o.value === NO_EPIC ? (
    <>
      <span className="h-3 w-3 shrink-0" />
      <span className="truncate italic text-slate-400">{o.label}</span>
    </>
  ) : (
    <>
      <Layers aria-hidden="true" className="h-3 w-3 shrink-0 text-slate-500" />
      {plain(o)}
    </>
  );
const drawRepo = (o: SelectOption) => <span className="truncate font-mono text-[11px]">{o.label}</span>;

const UNMATCHED_MODES: readonly { mode: UnmatchedMode; label: string }[] = [
  { mode: "dim", label: "Dim" },
  { mode: "hide", label: "Hide" },
];

/**
 * Dependencies' choice between dimming and hiding the cards the filters don't
 * match. It has no visible label, only a tooltip, and its radios are native
 * ones, visually hidden, so the arrow keys move between them. Dim, the
 * default, looks like an unset field; Hide stands out in blue like a set one.
 */
function UnmatchedToggle({ mode, onChange }: { mode: UnmatchedMode; onChange: (mode: UnmatchedMode) => void }) {
  const name = useId();
  const set = mode !== "dim";
  return (
    <div
      role="radiogroup"
      aria-label="Cards the filters don't match"
      title="Cards the filters don't match: dim them or hide them"
      className={segmentedClass(set)}
    >
      {UNMATCHED_MODES.map((option) => (
        <label key={option.mode} className={segmentClass(option.mode === mode, set)}>
          <input
            type="radio"
            name={name}
            value={option.mode}
            checked={option.mode === mode}
            onChange={() => onChange(option.mode)}
            className="sr-only"
          />
          {option.label}
        </label>
      ))}
    </div>
  );
}

/**
 * The filter bar shared by Dependencies, Kanban and Table. Its state is the
 * URL's (see useFilters); each page decides what a filter does to its tickets
 * and passes the counts to show. Every view always shows one project, so the
 * bar offers no "All projects" and picks one for a URL without it.
 */
export default function FilterPanel({
  state,
  tickets,
  repos,
  count,
  unmatched,
}: {
  state: FilterState;
  /**
   * Every project's tickets, which the pick reads each project's latest
   * activity from, or null until they have loaded. A failed first load that
   * the view settles on as none counts as loaded.
   */
  tickets: readonly ActivityTicket[] | null;
  /** The repos to offer, from the loaded tickets. */
  repos: string[];
  /** Matching and total tickets, once loaded. */
  count?: { shown: number; total: number };
  /**
   * Whether the cards the filters don't match are dimmed or hidden, and how
   * to change it; given by Dependencies only, which is the only view that
   * shows the choice. It is no filter, so Clear filters leaves it alone.
   */
  unmatched?: { mode: UnmatchedMode; onChange: (mode: UnmatchedMode) => void };
}) {
  const { filters, active, latestFilters, setFilter, dropValues, clearFilters } = state;
  // The projects and labels the bar offers, and checks the URL's filters
  // against. The bar loads them itself rather than being handed them, so that
  // an empty list means what it says: deleting the last project has to drop a
  // filter naming it, while a list still on its way must leave it alone. Null
  // is "not loaded", which is where a failed load leaves it too.
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [labels, setLabels] = useState<Label[] | null>(null);
  // The epics of the project shown, with the prefix they were loaded for, so
  // a list that belongs to the project just left is never taken for the new
  // one's. Null until the first load.
  const [epics, setEpics] = useState<{ project: string; epics: Epic[] } | null>(null);

  // The search box keeps what was typed in its own state. The URL only
  // catches up inside a transition, and an input whose value arrives that way
  // loses its caret; the URL also drops blank text, which the box must keep.
  // When the URL's search changes, the box follows only if the change came
  // from outside (a pasted URL, back or forward): the new value is the URL's
  // latest one and not what the box already holds. An older value of our own
  // arriving while a newer one is on its way is not the latest, so it's
  // ignored.
  const [search, setSearch] = useState(filters.q);
  const [seenQ, setSeenQ] = useState(filters.q);
  if (filters.q !== seenQ) {
    setSeenQ(filters.q);
    if (urlValue(search) !== filters.q && latestFilters().q === filters.q) setSearch(filters.q);
  }

  // Reloaded on every change, not only on mount, so a project or label
  // deleted elsewhere leaves the dropdowns — and takes a filter naming it
  // with it — without a reload. A failed load keeps whatever had loaded
  // before, and leaves a first one unloaded rather than claiming there is
  // nothing.
  const loadOptions = useCallback(() => {
    api.projects
      .list()
      .then((p) => setProjects(p ?? []))
      .catch(() => {});
    api.labels
      .list()
      .then((l) => setLabels(l ?? []))
      .catch(() => {});
  }, []);
  useEffect(() => loadOptions(), [loadOptions]);
  useLiveRefresh(loadOptions);

  // Filters that no longer name anything are dropped from the URL (below, and
  // the project in ProjectSelect), here rather than in each view, since the
  // bar is where the projects, epics and labels are known.
  const projectNames = useMemo(() => projects?.map((p) => p.prefix) ?? null, [projects]);
  const labelNames = useMemo(() => labels?.map((l) => l.name) ?? null, [labels]);

  // The project the URL names, as the list spells it, or null when it names
  // none that exists. Picking one for a URL without it, offering the
  // dropdown's entries, remembering the one shown and dropping a stale one
  // are ProjectSelect's job; the bar only needs to know which project its
  // epics belong to, and whether any project is active to show.
  const shownProject = projectNames && namedProject(projectNames, filters.project);
  const noActiveProject = useMemo(() => projects !== null && activeProjects(projects).length === 0, [projects]);

  // The epic filter offers the shown project's epics, loaded when it changes
  // and on every live change, like the projects and labels. An answer for a
  // project no longer shown is ignored.
  const epicsFor = useRef<string | null>(null);
  const loadEpics = useCallback(() => {
    const project = epicsFor.current;
    if (!project) return;
    api.epics
      .list(project)
      .then((list) => {
        if (epicsFor.current === project) setEpics({ project, epics: list?.epics ?? [] });
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    epicsFor.current = shownProject;
    loadEpics();
  }, [shownProject, loadEpics]);
  useLiveRefresh(loadEpics);
  // The shown project's epics, or null while they are unknown. With no active
  // project left to show there are none at all.
  const shownEpics = useMemo(() => {
    if (shownProject) return epics && epics.project === shownProject ? epics.epics : null;
    return noActiveProject ? [] : null;
  }, [epics, shownProject, noActiveProject]);

  // An epic is dropped from the filter once it names no epic of the shown
  // project, which is also what drops it on a switch to another project, and a
  // label once it names no label; the filter keeps its other values. A stale
  // project is ProjectSelect's to replace, or drop when none is left to pick.
  const epicNames = useMemo(() => shownEpics?.map((e) => e.name) ?? null, [shownEpics]);
  const stale = useMemo(
    () =>
      staleFilters(filters, { projects: projectNames, epics: epicNames, labels: labelNames }).filter(
        ({ key }) => key !== "project",
      ),
    [filters, projectNames, epicNames, labelNames],
  );
  useEffect(() => dropValues(stale), [stale, dropValues]);

  // "No epic", then the shown project's epics by name.
  const epicOptions = useMemo(
    () => [
      { value: NO_EPIC, label: "No epic" },
      ...[...(shownEpics ?? [])]
        .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }))
        .map((e) => ({ value: e.name, label: e.name })),
    ],
    [shownEpics],
  );

  const labelOptions = useMemo(() => (labels ?? []).map((l) => ({ value: l.name, label: l.name })), [labels]);
  const labelColors = useMemo(() => new Map((labels ?? []).map((l) => [l.name, l.color])), [labels]);
  const drawLabel = (o: SelectOption) => {
    const color = labelColors.get(o.value);
    return (
      <span
        className={`inline-flex min-w-0 items-center rounded px-1.5 py-0.5 text-[10.5px] font-medium ${color ? "" : "bg-slate-700/40 text-slate-300"}`}
        style={color ? { backgroundColor: color + "1f", color } : undefined}
      >
        <span className="truncate">{o.label}</span>
      </span>
    );
  };
  const repoList = useMemo(() => repos.map((r) => ({ value: r, label: r })), [repos]);

  const set = (key: MultiFilterKey) => (values: string[]) => setFilter(key, values);
  const clear = () => {
    setSearch("");
    clearFilters();
  };

  return (
    <div
      role="search"
      aria-label="Filter tickets"
      className="shrink-0 flex flex-wrap items-center gap-2 px-6 py-3 border-b border-slate-800/50"
    >
      <ProjectSelect state={state} projects={projects} tickets={tickets} />
      <FilterMultiSelect
        name="Epic"
        allLabel="All epics"
        values={filters.epic}
        options={epicOptions}
        ignoreCase
        divideAfter={NO_EPIC}
        renderOption={drawEpic}
        onChange={set("epic")}
      />
      <FilterMultiSelect
        name="Status"
        allLabel="All statuses"
        values={filters.status}
        options={STATUS_OPTIONS}
        renderOption={drawStatus}
        onChange={set("status")}
      />
      <FilterMultiSelect
        name="Priority"
        allLabel="All priorities"
        values={filters.priority}
        options={PRIORITY_OPTIONS}
        renderOption={drawPriority}
        onChange={set("priority")}
      />
      <FilterMultiSelect
        name="Label"
        allLabel="All labels"
        values={filters.label}
        options={labelOptions}
        ignoreCase
        renderOption={drawLabel}
        onChange={set("label")}
      />
      <FilterMultiSelect
        name="Repo"
        allLabel="All repos"
        values={filters.repo}
        options={repoList}
        renderOption={drawRepo}
        onChange={set("repo")}
      />
      <div className="relative">
        <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-500 pointer-events-none" />
        <input
          type="search"
          aria-label="Search"
          placeholder="Search key, title, description"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setFilter("q", e.target.value);
          }}
          className={`${fieldClass(filters.q !== "")} w-54 pl-7 placeholder:text-slate-500`}
        />
      </div>
      {unmatched && <UnmatchedToggle mode={unmatched.mode} onChange={unmatched.onChange} />}
      {active && (
        <button
          type="button"
          onClick={clear}
          className={CONTROL_BUTTON}
        >
          <X className="w-3.5 h-3.5" />
          Clear filters
        </button>
      )}
      {count && (
        <span className="text-xs text-slate-600 ml-auto" aria-live="polite">
          {active ? `${count.shown} of ${count.total}` : count.total} ticket{count.total !== 1 ? "s" : ""}
        </span>
      )}
    </div>
  );
}
