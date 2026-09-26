// The project a view shows when its URL names none.
//
// Every view (Dependencies, Kanban, Table) always shows one project: there is
// no "All projects". A URL without a `project` (a first visit, a link or
// bookmark without one, or one whose project was deleted and dropped) gets
// one picked for it, never an archived one: the project last shown on any
// view, remembered in localStorage, if it is still active; else the active
// project whose tickets changed most recently; else, on a tie or with no
// tickets at all, the first active project by name. With no active projects
// there is nothing to pick, and the view shows its empty state.
//
// localStorage can be missing or throw (private windows, blocked site data),
// so every access is wrapped: storage that can't be used just means there is
// no memory.

/** The localStorage key holding the last project shown, by prefix. */
export const LAST_PROJECT_KEY = "taskboard.lastProject";

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/** The prefix in `prefixes` that `value` names, ignoring case, as the filters compare it. */
export function namedProject(prefixes: readonly string[], value: string | null): string | null {
  if (!value) return null;
  return prefixes.find((prefix) => same(prefix, value)) ?? null;
}

/** The fields of a project the pick and the dropdown read. */
export interface PickableProject {
  prefix: string;
  name: string;
  status: string;
}

/** The fields of a ticket the pick reads its project's activity from. */
export interface ActivityTicket {
  projectPrefix: string;
  updatedAt: string;
}

/** Whether a project is archived: never picked for a view, and not offered in its dropdown. */
export function isArchived(project: Pick<PickableProject, "status">): boolean {
  return project.status === "archived";
}

/**
 * A project's icon and name, space-separated: the API leaves out an empty
 * icon, so it can be missing as well as blank, and the join has no leading
 * space when there is none. Every place that prints a project's icon next to
 * its name builds on this, so a project without one never shows a gap or an
 * "undefined".
 */
export function projectIconAndName(project: { name: string; icon?: string }): string {
  return [project.icon, project.name].filter(Boolean).join(" ");
}

/**
 * A project's option label: its icon and name (see projectIconAndName), with
 * " (archived)" appended for an archived project. For a place that already
 * shows archived status some other way (a status badge, say), use
 * projectIconAndName instead so it doesn't get the suffix twice.
 */
export function projectLabel(project: Pick<PickableProject, "name" | "status"> & { icon?: string }): string {
  const base = projectIconAndName(project);
  return isArchived(project) ? `${base} (archived)` : base;
}

/** Projects in alphabetical order by name, ignoring case; the prefix settles equal names. */
function byName(a: PickableProject, b: PickableProject): number {
  return a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.prefix.localeCompare(b.prefix);
}

/**
 * The projects a view's dropdown offers, and picks from: the active ones, in
 * alphabetical order by name ignoring case. A URL naming an archived project
 * still shows it, as an extra option after these labelled with its icon and
 * name and marked "(archived)".
 */
export function activeProjects<P extends PickableProject>(projects: readonly P[]): P[] {
  return projects.filter((p) => !isArchived(p)).sort(byName);
}

/**
 * When each project's tickets last changed: the latest `updatedAt` among its
 * tickets of any status, in milliseconds, keyed by the prefix in lowercase.
 * A project with no tickets, or none with a readable time, has no entry.
 */
export function latestActivity(tickets: readonly ActivityTicket[]): Map<string, number> {
  const latest = new Map<string, number>();
  for (const t of tickets) {
    const at = Date.parse(t.updatedAt);
    if (Number.isNaN(at)) continue;
    const key = t.projectPrefix.toLowerCase();
    const seen = latest.get(key);
    if (seen === undefined || at > seen) latest.set(key, at);
  }
  return latest;
}

/**
 * The project to show when the URL names none that exists, as its prefix: the
 * remembered one if it exists and is active; else the active project whose
 * tickets changed most recently (see latestActivity); else, on a tie or when
 * no active project has tickets, the first active project by name. "" when no
 * project is active.
 */
export function defaultProject(
  projects: readonly PickableProject[],
  remembered: string | null,
  activity: ReadonlyMap<string, number>,
): string {
  const active = activeProjects(projects);
  const kept = namedProject(active.map((p) => p.prefix), remembered);
  if (kept) return kept;
  // In name order, so the first of several equally recent projects wins.
  let latest: { prefix: string; at: number } | null = null;
  for (const p of active) {
    const at = activity.get(p.prefix.toLowerCase());
    if (at !== undefined && (latest === null || at > latest.at)) latest = { prefix: p.prefix, at };
  }
  return latest?.prefix ?? active[0]?.prefix ?? "";
}

/** The remembered project's prefix, or null when there is none or storage can't be read. */
export function readLastProject(): string | null {
  try {
    return globalThis.localStorage?.getItem(LAST_PROJECT_KEY) ?? null;
  } catch {
    return null;
  }
}

/** Remembers the project shown, for the next view without one. Storage that can't be written is ignored. */
export function rememberProject(prefix: string): void {
  try {
    globalThis.localStorage?.setItem(LAST_PROJECT_KEY, prefix);
  } catch {
    // No storage, no memory.
  }
}

/**
 * Whether a view is still waiting for its project to be picked: the URL names
 * none yet, and there are active projects to pick from (or the list is still
 * on its way). A view shows its loading state meanwhile rather than every
 * project's tickets. With no active projects nothing will be picked, and the
 * view shows its empty state.
 *
 * `failed` is whether the projects list has never loaded because its only
 * attempt so far failed (passed only together with `projects === null`,
 * which a load still on its way also leaves true). Waiting stops there too,
 * rather than forever: the view shows its load-error explanation instead,
 * and the next live refresh retries the load on its own.
 */
export function awaitingProject(
  project: string,
  projects: readonly Pick<PickableProject, "status">[] | null,
  failed = false,
): boolean {
  if (project !== "") return false;
  if (projects !== null) return projects.some((p) => !isArchived(p));
  return !failed;
}
