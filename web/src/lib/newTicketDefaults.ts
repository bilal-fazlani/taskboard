// What the new-ticket form starts with on a view.
//
// Every view shows one project (see defaultProject.ts), so a ticket created
// there starts in that project; otherwise it could land in another one and
// vanish from the view it was created on. The form reads its starting values
// from the view's filters, so a field the filters can also name starts the
// same way by adding it here. Every value stays changeable in the form.

import { isNoEpic, type Filters } from "./filters";

/** The form fields a view's filters preset. */
export interface NewTicketDefaults {
  /** The project's id, or "" for none. */
  projectId: string;
  /** The epic's id, or "" for none. */
  epicId: string;
}

/** The fields of a project the defaults read. */
export interface DefaultableProject {
  id: string;
  prefix: string;
}

/** A project's epics, as far as the form has loaded them. */
export interface ProjectEpics {
  projectId: string;
  epics: readonly { id: string; name: string }[];
}

/**
 * The form's starting values for a view with the given filters. The project is
 * the one the view shows, named by prefix and matched ignoring case as the
 * filters match it. A view without one, or with one that names no project,
 * falls back to the first project listed.
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
  const shown = filters.project.toLowerCase();
  const project = (shown && projects.find((p) => p.prefix.toLowerCase() === shown)) || projects[0];
  const projectId = project?.id ?? "";
  const wanted = filters.epic.length === 1 ? filters.epic[0].toLowerCase() : "";
  const epic =
    wanted && !isNoEpic(wanted) && epics && epics.projectId === projectId
      ? epics.epics.find((e) => e.name.toLowerCase() === wanted)
      : undefined;
  return { projectId, epicId: epic?.id ?? "" };
}
