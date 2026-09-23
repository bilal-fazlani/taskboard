import { useEffect, useMemo } from "react";
import type { Project } from "../api/client";
import type { FilterState } from "../hooks/useFilters";
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
import { selectOptions } from "../lib/filters";

const CONTROL =
  "bg-slate-800 text-xs rounded-md border px-2.5 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500";

/**
 * The project dropdown, used by the filter bar (FilterPanel) and on its own by
 * the Epics view, which has no other filters. The rules are defaultProject.ts's:
 * the URL's `project` names the project, a URL without one or with one that no
 * longer exists gets an active project picked, archived projects are offered
 * only when the URL names one, and the project shown is remembered for the
 * next view.
 */
export default function ProjectSelect({
  state,
  projects,
  tickets,
}: {
  state: FilterState;
  /** Every project, or null until they have loaded. */
  projects: readonly Project[] | null;
  /** Every project's tickets, which the pick reads activity from, or null until loaded. */
  tickets: readonly ActivityTicket[] | null;
}) {
  const { filters, setFilter, dropFilters } = state;
  const prefixes = useMemo(() => projects?.map((p) => p.prefix) ?? null, [projects]);
  const offered = useMemo(() => (projects ? activeProjects(projects) : null), [projects]);
  const shownProject = prefixes && namedProject(prefixes, filters.project);

  // With no active project left to pick, a project the URL names that no
  // longer exists is dropped rather than replaced.
  const stale = offered?.length === 0 && filters.project !== "" && shownProject === null;
  useEffect(() => {
    if (stale) dropFilters(["project"]);
  }, [stale, dropFilters]);

  // Nothing is picked until both the projects and the tickets whose activity
  // decides have loaded.
  const activity = useMemo(() => (tickets ? latestActivity(tickets) : null), [tickets]);
  const needsProject = offered !== null && offered.length > 0 && shownProject === null;
  useEffect(() => {
    if (needsProject && projects && activity) {
      setFilter("project", defaultProject(projects, readLastProject(), activity));
    }
  }, [needsProject, projects, activity, setFilter]);
  useEffect(() => {
    if (shownProject) rememberProject(shownProject);
  }, [shownProject]);

  const options = useMemo(() => {
    // The API leaves out an empty icon, so it can be missing as well as blank.
    const label = (p: Project) => [p.icon, p.name].filter(Boolean).join(" ");
    const entries = (offered ?? []).map((p) => ({ value: p.prefix, label: label(p) }));
    const archived = projects?.find((p) => p.prefix === shownProject && isArchived(p));
    if (archived) entries.push({ value: archived.prefix, label: `${label(archived)} (archived)` });
    return entries;
  }, [offered, projects, shownProject]);

  const shown = selectOptions(options, filters.project, true);
  const placeholder = offered?.length === 0 ? (projects?.length ? "No active projects" : "No projects") : "Project";

  return (
    <select
      aria-label="Project"
      value={shown.value}
      onChange={(e) => setFilter("project", e.target.value)}
      className={`${CONTROL} ${filters.project !== "" ? "border-blue-500/60 text-slate-100" : "border-slate-700 text-slate-300"}`}
    >
      {shown.value === "" && (
        <option value="" disabled>
          {placeholder}
        </option>
      )}
      {shown.options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
