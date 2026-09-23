// What the new-ticket form starts with on a view.
//
// Every view shows one project (see defaultProject.ts), so a ticket created
// there starts in that project; otherwise it could land in another one and
// vanish from the view it was created on. The form reads its starting values
// from the view's filters, so a field the filters can also name starts the
// same way by adding it here. Every value stays changeable in the form.

import type { Filters } from "./filters";

/** The form fields a view's filters preset. */
export interface NewTicketDefaults {
  /** The project's id, or "" for none. */
  projectId: string;
}

/** The fields of a project the defaults read. */
export interface DefaultableProject {
  id: string;
  prefix: string;
}

/**
 * The form's starting values for a view with the given filters. The project is
 * the one the view shows, named by prefix and matched ignoring case as the
 * filters match it. A view without one, or with one that names no project,
 * falls back to the first project listed.
 */
export function newTicketDefaults(
  filters: Pick<Filters, "project">,
  projects: readonly DefaultableProject[],
): NewTicketDefaults {
  const shown = filters.project.toLowerCase();
  const project = (shown && projects.find((p) => p.prefix.toLowerCase() === shown)) || projects[0];
  return { projectId: project?.id ?? "" };
}
