// The filters shared by the three views (Dependencies, Kanban and Table).
//
// Filter state lives in the URL, one query parameter per filter, omitted when
// empty, so a filtered view is bookmarkable and survives a reload. The same
// parameters apply on every view, so switching views carries them over.
// Every other query parameter (a `ticket` one, for example) is left alone.
//
// The project and the search take a single value. Every other filter takes
// several, as a repeated parameter, and a ticket matches it when it has any of
// them: /kanban?project=ACP&status=todo&status=in_progress&label=web.
// The project is named by its prefix rather than its id so the URL reads well.
// The epic is named by its name, or `none` for tickets without one.
// Values are kept as written: dropping ones that no longer name a project,
// epic or label is a separate concern (see staleFilters.ts).

export interface Filters {
  project: string;
  /** Epics of the project by name, or NO_EPIC for tickets without one. */
  epic: string[];
  status: string[];
  priority: string[];
  label: string[];
  repo: string[];
  /**
   * Free text, matched against the ticket's key, title and description, and
   * through the server against its documents' names and readable text.
   */
  q: string;
}

export type FilterKey = keyof Filters;

/** The filters that take several values, each written as a repeated parameter. */
export type MultiFilterKey = "epic" | "status" | "priority" | "label" | "repo";

export const MULTI_FILTER_KEYS: readonly MultiFilterKey[] = ["epic", "status", "priority", "label", "repo"];

/** Whether a filter takes several values. */
export function isMultiFilter(key: FilterKey): key is MultiFilterKey {
  return (MULTI_FILTER_KEYS as readonly FilterKey[]).includes(key);
}

/** One value of one filter, as the URL writes it. */
export interface FilterValue {
  key: FilterKey;
  value: string;
}

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

export const EMPTY_FILTERS: Filters = {
  project: "",
  epic: [],
  status: [],
  priority: [],
  label: [],
  repo: [],
  q: "",
};

/** The fields of a ticket that filtering reads. */
export interface FilterableTicket {
  /** Needed only to match the search through the ticket's documents. */
  id?: string;
  number: number;
  title: string;
  // description, repos and labels are normalised by the API client
  // (api/client.ts) to always be a string or an array, even though the API
  // itself leaves them out of the JSON when they're empty (ACP-51); nothing
  // here needs to guard against them being missing.
  description: string;
  status: string;
  priority: string;
  projectPrefix: string;
  repos: string[];
  labels: readonly { name: string }[];
  /** The API leaves it out when the ticket has no epic. */
  epic?: { name: string } | null;
}

/** The values, without empty ones and without repeats, in their order. */
const distinct = (values: readonly string[]) => [...new Set(values.filter((v) => v !== ""))];

export function parseFilters(params: URLSearchParams): Filters {
  const filters: Filters = { ...EMPTY_FILTERS, project: params.get("project") ?? "", q: params.get("q") ?? "" };
  for (const key of MULTI_FILTER_KEYS) filters[key] = distinct(params.getAll(key));
  return filters;
}

/** Whether a filter is set: a value, or at least one value for a multi-value filter. */
export function isSet(filters: Filters, key: FilterKey): boolean {
  const value = filters[key];
  return typeof value === "string" ? value !== "" : value.length > 0;
}

/** Whether any of the given filters (every filter by default) is set. */
export function hasFilters(filters: Filters, keys: readonly FilterKey[] = FILTER_KEYS): boolean {
  return keys.some((key) => isSet(filters, key));
}

/** What a filter control's value stores in the URL: nothing for blank text. */
export function urlValue(value: string): string {
  return value.trim() === "" ? "" : value;
}

/**
 * The params with one filter set, or removed when the value is empty (or only
 * spaces, for the search). A multi-value filter is written as one parameter
 * per value, in the order given, where its first parameter was; with no values
 * it is removed. Other parameters and their order are kept.
 */
export function withFilter<K extends FilterKey>(params: URLSearchParams, key: K, value: Filters[K]): URLSearchParams {
  const values = typeof value === "string" ? distinct([urlValue(value)]) : distinct(value);
  const next = new URLSearchParams();
  let placed = false;
  for (const [k, v] of params) {
    if (k !== key) next.append(k, v);
    else if (!placed) {
      placed = true;
      for (const stored of values) next.append(key, stored);
    }
  }
  if (!placed) for (const stored of values) next.append(key, stored);
  return next;
}

/**
 * The params without the given values, each removed only from its own
 * filter and only where the URL has it exactly so; the filters' other values
 * and every other parameter are kept.
 */
export function withoutValues(params: URLSearchParams, drop: readonly FilterValue[]): URLSearchParams {
  const next = new URLSearchParams();
  for (const [k, v] of params) {
    if (!drop.some((d) => d.key === k && d.value === v)) next.append(k, v);
  }
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
    const values = isMultiFilter(key) ? distinct(params.getAll(key)) : [params.get(key) ?? ""];
    for (const value of values) if (value) kept.append(key, value);
  }
  if (parseUnmatched(params) === "hide") kept.set(UNMATCHED_PARAM, "hide");
  const qs = kept.toString();
  return qs ? `?${qs}` : "";
}

export function ticketKey(ticket: Pick<FilterableTicket, "projectPrefix" | "number">): string {
  return `${ticket.projectPrefix}-${ticket.number}`;
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/** Whether a filter with the given values lets a ticket through: it's unset, or the ticket has any of them. */
const anyOf = (chosen: readonly string[], has: (value: string) => boolean) => chosen.length === 0 || chosen.some(has);

/**
 * Whether a ticket passes every filter that is set, and a multi-value filter
 * when it has any of its values. Project, epic, label and the search
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
  const { epic } = ticket;
  if (!anyOf(filters.epic, (e) => (isNoEpic(e) ? !epic : !!epic && same(epic.name, e)))) return false;
  if (!anyOf(filters.status, (s) => ticket.status === s)) return false;
  if (!anyOf(filters.priority, (p) => ticket.priority === p)) return false;
  if (!anyOf(filters.label, (name) => ticket.labels.some((l) => same(l.name, name)))) return false;
  if (!anyOf(filters.repo, (r) => ticket.repos.includes(r))) return false;
  const q = filters.q.trim().toLowerCase();
  if (q) {
    const haystacks = [ticketKey(ticket), ticket.title, ticket.description];
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
 * A dropdown's options and the value to show selected for a single-value
 * filter from the URL, the project. With `ignoreCase` (the project matches
 * ignoring case) a value in another case selects the option it matches; any
 * value that matches no option is added as one, so it still shows.
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
 * A multi-value control's options and the ones to show chosen for a filter's
 * values from the URL. With `ignoreCase` (for filters that match ignoring
 * case, epic and label) a value in another case chooses the option it
 * matches; a value that matches no option is added as one, so it still shows.
 * The chosen ones are in option order.
 */
export function multiSelectOptions(
  options: readonly SelectOption[],
  chosen: readonly string[],
  ignoreCase = false,
): { options: SelectOption[]; chosen: string[] } {
  const all = [...options];
  const picked = new Set<string>();
  for (const value of chosen) {
    const match = all.find((o) => (ignoreCase ? o.value.toLowerCase() === value.toLowerCase() : o.value === value));
    if (match) picked.add(match.value);
    else {
      all.push({ value, label: value });
      picked.add(value);
    }
  }
  return { options: all, chosen: all.filter((o) => picked.has(o.value)).map((o) => o.value) };
}

/**
 * The values a multi-value control writes after one option is ticked or
 * unticked: the chosen ones with that one flipped, in option order, so the
 * same choice always makes the same URL.
 */
export function toggleValue(options: readonly SelectOption[], chosen: readonly string[], value: string): string[] {
  const next = new Set(chosen);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return options.filter((o) => next.has(o.value)).map((o) => o.value);
}

/**
 * The repos to offer: every repo on the given tickets, sorted, plus the
 * selected ones no ticket carries, so the control still shows them.
 */
export function repoOptions(tickets: readonly Pick<FilterableTicket, "repos">[], selected: readonly string[] = []): string[] {
  const repos = new Set<string>();
  for (const t of tickets) for (const r of t.repos) repos.add(r);
  for (const r of selected) if (r) repos.add(r);
  return [...repos].sort((a, b) => a.localeCompare(b));
}
