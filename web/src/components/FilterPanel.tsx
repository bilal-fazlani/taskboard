import { useCallback, useEffect, useMemo, useState } from "react";
import { Search, X } from "lucide-react";
import { api, type Label, type Project } from "../api/client";
import type { FilterState } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import {
  activeProjects,
  defaultProject,
  isArchived,
  latestActivity,
  namedProject,
  readLastProject,
  rememberProject,
  type ActivityTicket,
} from "../lib/defaultProject";
import { selectOptions, urlValue, type FilterKey, type SelectOption } from "../lib/filters";
import { PRIORITIES } from "../lib/priority";
import { staleFilters } from "../lib/staleFilters";
import { STATUSES, STATUS_LABELS } from "../lib/status";

const CONTROL =
  "bg-slate-800 text-xs rounded-md border px-2.5 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500";
// A set filter stands out from the unset ones.
const controlClass = (set: boolean) =>
  `${CONTROL} ${set ? "border-blue-500/60 text-slate-100" : "border-slate-700 text-slate-300"}`;

function FilterSelect({
  name,
  allLabel,
  placeholder,
  value,
  options,
  ignoreCase,
  onChange,
}: {
  name: string;
  /** The label of the empty value that sets no filter. Without one there is no such option. */
  allLabel?: string;
  /** Without an allLabel, what shows while the value is empty, as a disabled option. */
  placeholder?: string;
  value: string;
  options: SelectOption[];
  /** Whether the filter matches ignoring case, so a URL value in another case selects its option. */
  ignoreCase?: boolean;
  onChange: (value: string) => void;
}) {
  const shown = selectOptions(options, value, ignoreCase);
  return (
    <select
      aria-label={name}
      value={shown.value}
      onChange={(e) => onChange(e.target.value)}
      className={controlClass(value !== "")}
    >
      {allLabel !== undefined ? (
        <option value="">{allLabel}</option>
      ) : (
        shown.value === "" && (
          <option value="" disabled>
            {placeholder}
          </option>
        )
      )}
      {shown.options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
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
}) {
  const { filters, active, latestFilters, setFilter, dropFilters, clearFilters } = state;
  // The projects and labels the bar offers, and checks the URL's filters
  // against. The bar loads them itself rather than being handed them, so that
  // an empty list means what it says: deleting the last project has to drop a
  // filter naming it, while a list still on its way must leave it alone. Null
  // is "not loaded", which is where a failed load leaves it too.
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [labels, setLabels] = useState<Label[] | null>(null);

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

  // Filters that no longer name anything are dropped from the URL, here
  // rather than in each view, since the bar is where the projects and labels
  // are known.
  const projectNames = useMemo(() => projects?.map((p) => p.prefix) ?? null, [projects]);
  const labelNames = useMemo(() => labels?.map((l) => l.name) ?? null, [labels]);
  // The projects the dropdown offers and the pick chooses from: the active
  // ones, by name. A URL naming an archived one still shows it, as an extra
  // entry, and keeps it.
  const offered = useMemo(() => (projects ? activeProjects(projects) : null), [projects]);
  // A view always shows one project, so while there are active projects a
  // stale one is replaced below rather than dropped; only with none left to
  // pick does it go.
  const stale = useMemo(
    () =>
      staleFilters(filters, { projects: projectNames, labels: labelNames }).filter(
        (key) => key !== "project" || offered?.length === 0,
      ),
    [filters, projectNames, labelNames, offered],
  );
  useEffect(() => dropFilters(stale), [stale, dropFilters]);

  // The project the URL names, as the list spells it, or null when it names
  // none that exists. An archived one counts: a link or bookmark to it still
  // opens it. Every view always has one: a URL without one, or with one that
  // was deleted, gets an active project picked (see defaultProject.ts),
  // replacing the history entry like any filter change. Nothing is picked
  // until both the projects and the tickets whose activity decides have
  // loaded, so the pick never runs on part of the data, or when no project is
  // active.
  const shownProject = projectNames && namedProject(projectNames, filters.project);
  const activity = useMemo(() => (tickets ? latestActivity(tickets) : null), [tickets]);
  const needsProject = offered !== null && offered.length > 0 && shownProject === null;
  useEffect(() => {
    if (needsProject && projects && activity) {
      setFilter("project", defaultProject(projects, readLastProject(), activity));
    }
  }, [needsProject, projects, activity, setFilter]);
  // The project shown is the one the next view without one starts on.
  useEffect(() => {
    if (shownProject) rememberProject(shownProject);
  }, [shownProject]);

  // The dropdown's entries: the active projects, plus an archived one the URL
  // names, marked as such, after them. A value naming no project at all still
  // shows as written (see selectOptions).
  const projectOptions = useMemo(() => {
    // The API leaves out an empty icon, so it can be missing as well as blank.
    const label = (p: Project) => [p.icon, p.name].filter(Boolean).join(" ");
    const options = (offered ?? []).map((p) => ({ value: p.prefix, label: label(p) }));
    const archived = projects?.find((p) => p.prefix === shownProject && isArchived(p));
    if (archived) options.push({ value: archived.prefix, label: `${label(archived)} (archived)` });
    return options;
  }, [offered, projects, shownProject]);

  const set = (key: FilterKey) => (value: string) => setFilter(key, value);
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
      <FilterSelect
        name="Project"
        placeholder={offered?.length === 0 ? (projects?.length ? "No active projects" : "No projects") : "Project"}
        value={filters.project}
        options={projectOptions}
        ignoreCase
        onChange={set("project")}
      />
      <FilterSelect
        name="Status"
        allLabel="All statuses"
        value={filters.status}
        options={STATUSES.map((s) => ({ value: s, label: STATUS_LABELS[s] }))}
        onChange={set("status")}
      />
      <FilterSelect
        name="Priority"
        allLabel="All priorities"
        value={filters.priority}
        options={PRIORITIES.map((p) => ({ value: p, label: p.charAt(0).toUpperCase() + p.slice(1) }))}
        onChange={set("priority")}
      />
      <FilterSelect
        name="Label"
        allLabel="All labels"
        value={filters.label}
        options={(labels ?? []).map((l) => ({ value: l.name, label: l.name }))}
        ignoreCase
        onChange={set("label")}
      />
      <FilterSelect
        name="Repo"
        allLabel="All repos"
        value={filters.repo}
        options={repos.map((r) => ({ value: r, label: r }))}
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
          className={`${controlClass(filters.q !== "")} w-60 pl-7 placeholder:text-slate-500`}
        />
      </div>
      {active && (
        <button
          type="button"
          onClick={clear}
          className="inline-flex items-center gap-1 px-2 py-1.5 text-xs text-slate-400 hover:text-slate-200 rounded-md hover:bg-slate-800 transition-colors"
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
