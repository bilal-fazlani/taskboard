// The filters shared by the three views (Dependencies, Kanban and Table).
//
// Filter state lives in the URL, one query parameter per filter, omitted when
// empty, so a filtered view is bookmarkable and survives a reload. The same
// parameters apply on every view, so switching views carries them over.
// Every other query parameter (a `ticket` one, for example) is left alone.
//
// Each filter takes a single value. The project is named by its prefix rather
// than its id so the URL reads well: /kanban?project=ACP&status=todo&label=web.
// The epic is named by its name, or `none` for tickets without one.
// Values are kept as written: dropping ones that no longer name a project or
// label is a separate concern.

export interface Filters {
  project: string;
  /** An epic of the project by name, or NO_EPIC for tickets without one. */
  epic: string;
  status: string;
  priority: string;
  label: string;
  repo: string;
  /**
   * Free text, matched against the ticket's key, title and description, and
   * through the server against its documents' names and readable text.
   */
  q: string;
}

export type FilterKey = keyof Filters;

/** The query parameter names, which are also the Filters fields, in display order. */
export const FILTER_KEYS: readonly FilterKey[] = ["project", "epic", "status", "priority", "label", "repo", "q"];

/**
 * The epic filter's value for tickets without an epic. No epic can be called
 * this, in any case, so it never clashes with a name.
 */
export const NO_EPIC = "none";

/** Whether an epic filter value means "no epic", in any case. */
export const isNoEpic = (value: string) => value.toLowerCase() === NO_EPIC;

/**
 * The filters that narrow down a project's tickets: every one but the project,
 * which every view always has set (see defaultProject.ts). These are the ones
 * that make a view "filtered", and the ones Clear filters removes.
 */
export const NARROWING_KEYS: readonly FilterKey[] = FILTER_KEYS.filter((key) => key !== "project");

export const EMPTY_FILTERS: Filters = { project: "", epic: "", status: "", priority: "", label: "", repo: "", q: "" };

/** The fields of a ticket that filtering reads. */
export interface FilterableTicket {
  /** Needed only to match the search through the ticket's documents. */
  id?: string;
  number: number;
  title: string;
  /** The API leaves out an empty description, and empty repos and labels. */
  description?: string;
  status: string;
  priority: string;
  projectPrefix: string;
  repos?: string[];
  labels?: readonly { name: string }[] | null;
  /** The API leaves it out when the ticket has no epic. */
  epic?: { name: string } | null;
}

export function parseFilters(params: URLSearchParams): Filters {
  const filters = { ...EMPTY_FILTERS };
  for (const key of FILTER_KEYS) filters[key] = params.get(key) ?? "";
  return filters;
}

/** Whether any of the given filters (every filter by default) is set. */
export function hasFilters(filters: Filters, keys: readonly FilterKey[] = FILTER_KEYS): boolean {
  return keys.some((key) => filters[key] !== "");
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

/**
 * The params without the given filters, or without any of them by default.
 * Every other parameter is kept.
 */
export function withoutFilters(params: URLSearchParams, keys: readonly FilterKey[] = FILTER_KEYS): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of keys) next.delete(key);
  return next;
}

// How Dependencies shows the cards the filters don't match: dimmed in place,
// the default, or left off the graph. It is a display mode rather than a
// filter, so it is no FilterKey: it doesn't make a view filtered, Clear
// filters leaves it, and it names nothing that can be deleted. It lives in the
// URL all the same, as `unmatched=hide` and only in hide mode, so a link
// shows what its sender saw; Kanban and Table, which always hide, ignore it.

export type UnmatchedMode = "dim" | "hide";

/** The query parameter that carries hide mode. */
export const UNMATCHED_PARAM = "unmatched";

/** The mode the params ask for: hide for `unmatched=hide`, and dim for anything else. */
export function parseUnmatched(params: URLSearchParams): UnmatchedMode {
  return params.get(UNMATCHED_PARAM) === "hide" ? "hide" : "dim";
}

/**
 * Whether the params carry an `unmatched` value that means nothing, such as
 * `dim` or a typo, which is dropped from the URL like any invalid value.
 */
export function hasInvalidUnmatched(params: URLSearchParams): boolean {
  return params.has(UNMATCHED_PARAM) && parseUnmatched(params) !== "hide";
}

/** The params in the given mode: `unmatched=hide` for hide, no parameter for dim. Others are kept. */
export function withUnmatched(params: URLSearchParams, mode: UnmatchedMode): URLSearchParams {
  const next = new URLSearchParams(params);
  if (mode === "hide") next.set(UNMATCHED_PARAM, "hide");
  else next.delete(UNMATCHED_PARAM);
  return next;
}

/**
 * Just the filter part of a query string, as `?…` or "" when there is none,
 * for links to another view. Hide mode goes along with the filters, so it's
 * still set on coming back to Dependencies. Other parameters stay behind with
 * the view they belong to.
 */
export function filterSearch(search: string): string {
  const params = new URLSearchParams(search);
  const kept = new URLSearchParams();
  for (const key of FILTER_KEYS) {
    const value = params.get(key);
    if (value) kept.set(key, value);
  }
  if (parseUnmatched(params) === "hide") kept.set(UNMATCHED_PARAM, "hide");
  const qs = kept.toString();
  return qs ? `?${qs}` : "";
}

export function ticketKey(ticket: Pick<FilterableTicket, "projectPrefix" | "number">): string {
  return `${ticket.projectPrefix}-${ticket.number}`;
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/**
 * Whether a ticket passes every filter. Project, epic, label and the search
 * compare case-insensitively; status and priority are fixed lowercase sets;
 * repos are matched exactly, as they are everywhere else. The epic filter's
 * NO_EPIC matches the tickets without an epic.
 *
 * `docMatches` holds the ids of the tickets whose documents match the search
 * (see useDocumentMatches); it widens only the search, never another filter.
 */
export function matchesFilters(
  ticket: FilterableTicket,
  filters: Filters,
  docMatches?: ReadonlySet<string> | null,
): boolean {
  if (filters.project && !same(ticket.projectPrefix, filters.project)) return false;
  if (filters.epic) {
    if (isNoEpic(filters.epic) ? ticket.epic : !ticket.epic || !same(ticket.epic.name, filters.epic)) return false;
  }
  if (filters.status && ticket.status !== filters.status) return false;
  if (filters.priority && ticket.priority !== filters.priority) return false;
  if (filters.label && !(ticket.labels ?? []).some((l) => same(l.name, filters.label))) return false;
  if (filters.repo && !(ticket.repos ?? []).includes(filters.repo)) return false;
  const q = filters.q.trim().toLowerCase();
  if (q) {
    const haystacks = [ticketKey(ticket), ticket.title, ticket.description ?? ""];
    const inText = haystacks.some((h) => h.toLowerCase().includes(q));
    const inDocuments = ticket.id !== undefined && docMatches?.has(ticket.id) === true;
    if (!inText && !inDocuments) return false;
  }
  return true;
}

/**
 * The tickets in the given project, ignoring case as the filter does. None
 * when no project is given: every view shows one project, and a view left
 * without one (no active project to pick) shows its empty state rather than
 * every project's tickets.
 */
export function inProject<T extends Pick<FilterableTicket, "projectPrefix">>(tickets: readonly T[], project: string): T[] {
  if (!project) return [];
  return tickets.filter((t) => same(t.projectPrefix, project));
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
