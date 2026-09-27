// What the new-ticket form starts with on a view, and whether the view can
// open it at all.
//
// Every view shows one project (see defaultProject.ts), so a ticket created
// there starts in that project; otherwise it could land in another one and
// vanish from the view it was created on. The form reads its starting values
// from the view's filters, so a field the filters can also name starts the
// same way by adding it here. Every value stays changeable in the form.

import { namedProject, projectsByName, type PickableProject } from "./defaultProject";
import { isNoEpic, type Filters } from "./filters";

/** The form fields a view's filters preset. */
export interface NewTicketDefaults {
  /** The project's id, or "" for none. */
  projectId: string;
  /** The epic's id, or "" for none. */
  epicId: string;
}

/** The fields of a project the defaults read. */
export interface DefaultableProject extends PickableProject {
  id: string;
}

/** A project's epics, as far as the form has loaded them. */
export interface ProjectEpics {
  projectId: string;
  epics: readonly { id: string; name: string }[];
}

/**
 * The form's starting values for a view with the given filters. The project is
 * picked the same way the filter bar picks one (see defaultProject.ts): among
 * the projects, sorted by name, the one the view shows, named by prefix and
 * matched ignoring case as the filters match it. A view without one, or with
 * one that names no project, falls back to the first project by name. With no
 * projects there is none to fall back to, and the form starts with no project.
 *
 * The epic is the one the view's epic filter names, matched by name ignoring
 * case among that project's epics, once they have loaded. A filter for tickets
 * without an epic, for several epics, or none at all, starts the form on no
 * epic: only a filter naming exactly one epic says which one a new ticket
 * belongs to.
 */
export function newTicketDefaults(
  filters: Pick<Filters, "project" | "epic">,
  projects: readonly DefaultableProject[],
  epics: ProjectEpics | null = null,
): NewTicketDefaults {
  const sorted = projectsByName(projects);
  const kept = namedProject(
    sorted.map((p) => p.prefix),
    filters.project,
  );
  const project = (kept && sorted.find((p) => p.prefix === kept)) || sorted[0];
  const projectId = project?.id ?? "";
  const wanted = filters.epic.length === 1 ? filters.epic[0].toLowerCase() : "";
  const epic =
    wanted && !isNoEpic(wanted) && epics && epics.projectId === projectId
      ? epics.epics.find((e) => e.name.toLowerCase() === wanted)
      : undefined;
  return { projectId, epicId: epic?.id ?? "" };
}

/**
 * Why a view can't open the new-ticket form, or null when it can. Every way to
 * open the form (Table's New Ticket button, each Kanban column's +) is disabled
 * with this reason on hover and for screen readers, so the form never opens
 * where it would have to guess a project:
 *
 * - while the projects are still loading (`projects` is null), since until
 *   then the view can't tell whether there is a project to put it in;
 * - when there are no projects, so there is none to put a ticket in.
 *
 * `failed` is whether the projects list has never loaded because its only
 * attempt so far failed, which changes only the message shown while
 * `projects` is null: a load still on its way says so, one that failed says
 * that instead, and the next live refresh retries it on its own.
 *
 * The Epics view's New epic follows the same rule, since a new epic belongs in
 * the view's project just as a new ticket does; `noun` names what the reason
 * says can't be added.
 */
export function newTicketBlocked(
  projects: readonly PickableProject[] | null,
  failed = false,
  noun: "tickets" | "epics" = "tickets",
): string | null {
  if (projects === null) return failed ? "Couldn't load projects. Retrying…" : "Loading projects…";
  if (projects.length === 0) return `Create a project to add ${noun}.`;
  return null;
}
