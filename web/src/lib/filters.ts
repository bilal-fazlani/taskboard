// The filters shared by the three views (Dependencies, Kanban and Table).
//
// Filter state lives in the URL, one query parameter per filter, omitted when
// empty, so a filtered view is bookmarkable and survives a reload. The same
// parameters apply on every view, so switching views carries them over.
// Every other query parameter (a `ticket` one, for example) is left alone.
//
// Each filter takes a single value. The project is named by its prefix rather
// than its id so the URL reads well: /kanban?project=ACP&status=todo&label=web.
// Values are kept as written: dropping ones that no longer name a project or
// label is a separate concern.

export interface Filters {
  project: string;
  status: string;
  priority: string;
  label: string;
  repo: string;
  /** Free text, matched against the ticket's key, title and description. */
  q: string;
}

export type FilterKey = keyof Filters;

/** The query parameter names, which are also the Filters fields, in display order. */
export const FILTER_KEYS: readonly FilterKey[] = ["project", "status", "priority", "label", "repo", "q"];

export const EMPTY_FILTERS: Filters = { project: "", status: "", priority: "", label: "", repo: "", q: "" };

/** The fields of a ticket that filtering reads. */
export interface FilterableTicket {
  number: number;
  title: string;
  /** The API leaves out an empty description, and empty repos and labels. */
  description?: string;
  status: string;
  priority: string;
  projectPrefix: string;
  repos?: string[];
  labels?: readonly { name: string }[] | null;
}

export function parseFilters(params: URLSearchParams): Filters {
  const filters = { ...EMPTY_FILTERS };
  for (const key of FILTER_KEYS) filters[key] = params.get(key) ?? "";
  return filters;
}

export function hasFilters(filters: Filters): boolean {
  return FILTER_KEYS.some((key) => filters[key] !== "");
}

/** What a filter control's value stores in the URL: nothing for blank text. */
export function urlValue(value: string): string {
  return value.trim() === "" ? "" : value;
}

/**
 * The params with one filter set, or removed when the value is empty (or only
 * spaces, for the search). Other parameters and their order are kept.
 */
export function withFilter(params: URLSearchParams, key: FilterKey, value: string): URLSearchParams {
  const next = new URLSearchParams(params);
  const stored = urlValue(value);
  if (stored === "") next.delete(key);
  else next.set(key, stored);
  return next;
}

/** The params without any filter; every other parameter is kept. */
export function withoutFilters(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of FILTER_KEYS) next.delete(key);
  return next;
}

/**
 * Just the filter part of a query string, as `?…` or "" when there is none,
 * for links to another view. Other parameters stay behind with the view they
 * belong to.
 */
export function filterSearch(search: string): string {
  const params = new URLSearchParams(search);
  const kept = new URLSearchParams();
  for (const key of FILTER_KEYS) {
    const value = params.get(key);
    if (value) kept.set(key, value);
  }
  const qs = kept.toString();
  return qs ? `?${qs}` : "";
}

export function ticketKey(ticket: Pick<FilterableTicket, "projectPrefix" | "number">): string {
  return `${ticket.projectPrefix}-${ticket.number}`;
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/**
 * Whether a ticket passes every filter. Project, label and the search compare
 * case-insensitively; status and priority are fixed lowercase sets; repos are
 * matched exactly, as they are everywhere else.
 */
export function matchesFilters(ticket: FilterableTicket, filters: Filters): boolean {
  if (filters.project && !same(ticket.projectPrefix, filters.project)) return false;
  if (filters.status && ticket.status !== filters.status) return false;
  if (filters.priority && ticket.priority !== filters.priority) return false;
  if (filters.label && !(ticket.labels ?? []).some((l) => same(l.name, filters.label))) return false;
  if (filters.repo && !(ticket.repos ?? []).includes(filters.repo)) return false;
  const q = filters.q.trim().toLowerCase();
  if (q) {
    const haystacks = [ticketKey(ticket), ticket.title, ticket.description ?? ""];
    if (!haystacks.some((h) => h.toLowerCase().includes(q))) return false;
  }
  return true;
}

export interface SelectOption {
  value: string;
  label: string;
}

/**
 * A dropdown's options and the value to show selected for a filter value from
 * the URL. With `ignoreCase` (for filters that match ignoring case, project and
 * label) a value in another case selects the option it matches; any value that
 * matches no option is added as one, so it still shows.
 */
export function selectOptions(
  options: SelectOption[],
  selected: string,
  ignoreCase = false,
): { options: SelectOption[]; value: string } {
  if (!selected) return { options, value: selected };
  const match = options.find((o) =>
    ignoreCase ? o.value.toLowerCase() === selected.toLowerCase() : o.value === selected,
  );
  if (match) return { options, value: match.value };
  return { options: [...options, { value: selected, label: selected }], value: selected };
}

/**
 * The repos to offer: every repo on the given tickets, sorted, plus the
 * selected one if no ticket carries it, so the control still shows it.
 */
export function repoOptions(tickets: readonly Pick<FilterableTicket, "repos">[], selected = ""): string[] {
  const repos = new Set<string>();
  for (const t of tickets) for (const r of t.repos ?? []) repos.add(r);
  if (selected) repos.add(selected);
  return [...repos].sort((a, b) => a.localeCompare(b));
}
