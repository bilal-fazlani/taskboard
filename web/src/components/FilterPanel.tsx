import { useEffect, useState } from "react";
import { Search, X } from "lucide-react";
import { api, type Label, type Project } from "../api/client";
import type { FilterState } from "../hooks/useFilters";
import { selectOptions, urlValue, type FilterKey, type SelectOption } from "../lib/filters";
import { PRIORITIES } from "../lib/priority";
import { STATUSES, STATUS_LABELS } from "../lib/status";

const CONTROL =
  "bg-slate-800 text-xs rounded-md border px-2.5 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500";
// A set filter stands out from the unset ones.
const controlClass = (set: boolean) =>
  `${CONTROL} ${set ? "border-blue-500/60 text-slate-100" : "border-slate-700 text-slate-300"}`;

function FilterSelect({
  name,
  allLabel,
  value,
  options,
  ignoreCase,
  onChange,
}: {
  name: string;
  allLabel: string;
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
      <option value="">{allLabel}</option>
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
 * and passes the counts to show.
 */
export default function FilterPanel({
  state,
  projects,
  repos,
  count,
}: {
  state: FilterState;
  projects: Project[];
  /** The repos to offer, from the loaded tickets. */
  repos: string[];
  /** Matching and total tickets, once loaded. */
  count?: { shown: number; total: number };
}) {
  const { filters, active, latestFilters, setFilter, clearFilters } = state;
  const [labels, setLabels] = useState<Label[]>([]);

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

  useEffect(() => {
    api.labels.list().then((l) => setLabels(l ?? [])).catch(() => setLabels([]));
  }, []);

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
        allLabel="All projects"
        value={filters.project}
        options={projects.map((p) => ({ value: p.prefix, label: `${p.icon} ${p.name}`.trim() }))}
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
        options={labels.map((l) => ({ value: l.name, label: l.name }))}
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
