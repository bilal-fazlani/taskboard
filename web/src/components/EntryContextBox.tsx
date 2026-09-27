// "Context around this ticket" in the ticket page's side column: its epic's
// and its project's entries as counts, each linking to that epic or project,
// rather than shown inline on every ticket.
import type { MouseEvent } from "react";
import { Link } from "react-router-dom";
import type { EntryContext } from "../hooks/useEntryContext";
import { countsPhrase } from "../lib/entries";

/**
 * The box. `onNavigate`, when given, is called instead of following a link
 * straight away, so the editor can ask about unsaved edits first.
 */
export default function EntryContextBox({
  context,
  epic,
  project,
  onNavigate,
}: {
  context: EntryContext | null;
  epic?: { name: string };
  project: { prefix: string; name?: string };
  onNavigate?: (to: string) => void;
}) {
  const epicTo = epic ? `/epics?${new URLSearchParams({ project: project.prefix, epic: epic.name }).toString()}` : "";
  const projectTo = `/projects?${new URLSearchParams({ project: project.prefix }).toString()}`;
  const follow = (to: string) => (e: MouseEvent<HTMLAnchorElement>) => {
    if (!onNavigate || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    onNavigate(to);
  };
  return (
    <div
      data-testid="entry-context"
      className="rounded-lg border border-slate-800 px-3 py-2.5 text-[13px] leading-relaxed text-slate-400"
    >
      <div className="font-semibold text-slate-200">Context around this ticket</div>
      {epic && (
        <div data-testid="entry-context-epic">
          Epic {epic.name}: {context?.epic ? countsPhrase(context.epic) : "…"} ·{" "}
          <Link to={epicTo} onClick={follow(epicTo)} className="text-blue-400 hover:text-blue-300">
            open
          </Link>
        </div>
      )}
      <div data-testid="entry-context-project">
        Project {project.prefix}: {context ? countsPhrase(context.project) : "…"} ·{" "}
        <Link to={projectTo} onClick={follow(projectTo)} className="text-blue-400 hover:text-blue-300">
          open
        </Link>
      </div>
    </div>
  );
}
