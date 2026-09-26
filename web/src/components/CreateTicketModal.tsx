import { useEffect, useId, useRef, useState } from "react";
import { X } from "lucide-react";
import { api, type TicketWrite, type Project } from "../api/client";
import ImageUploadStatus from "./ImageUploadStatus";
import LabelPicker from "./LabelPicker";
import { usePasteImages } from "../hooks/usePasteImages";
import { activeProjects } from "../lib/defaultProject";
import { DEFAULT_STATUS } from "../lib/status";
import type { Filters } from "../lib/filters";
import { newTicketDefaults, type ProjectEpics } from "../lib/newTicketDefaults";

const PRIORITIES = ["urgent", "high", "medium", "low"];

// A project's icon and name, as the filter bar shows them. The API leaves out
// an empty icon, so it can be missing as well as blank.
const projectLabel = (p: Project) => [p.icon, p.name].filter(Boolean).join(" ");

export default function CreateTicketModal({
  projects,
  filters,
  defaultStatus,
  onClose,
  onCreate,
}: {
  projects: Project[];
  /** The view's filters, which the form starts from: the project the view shows, and the epic it filters by. */
  filters: Filters;
  defaultStatus?: string;
  onClose: () => void;
  onCreate: (data: TicketWrite) => void;
}) {
  // The project the form is on: null until the projects are known, then the
  // one it starts on (the view's, see newTicketDefaults), and whichever the
  // user picks after that. The views open the form only once the projects
  // have loaded (see newTicketBlocked); handed none, it waits for them all
  // the same. A later refresh never moves the form to another project on its
  // own.
  const [chosenProjectId, setChosenProjectId] = useState<string | null>(null);
  // The epics of the project the form is on, tagged with it so a list for the
  // project just left is never offered for the new one.
  const [epics, setEpics] = useState<ProjectEpics | null>(null);
  // Null until the user picks an epic, and again when they pick another
  // project. Until then the form follows the view's epic filter, once the
  // project's epics have loaded, as long as it is on the view's project.
  const [pickedEpicId, setPickedEpicId] = useState<string | null>(null);
  const defaults = newTicketDefaults(filters, projects, epics);
  if (chosenProjectId === null && defaults.projectId !== "") setChosenProjectId(defaults.projectId);
  // Same offering as the filter bar's project dropdown (see defaultProject.ts):
  // active projects only, sorted by name. With none, the placeholder mirrors
  // ProjectSelect's rather than falling back to an archived project.
  const activeProjectsList = activeProjects(projects);
  // A project archived while the form is open (a live refresh brings the
  // change in) is no longer offered, so the form drops it and waits for
  // another pick rather than creating the ticket there.
  const chosen = chosenProjectId ?? defaults.projectId;
  const projectId = activeProjectsList.some((p) => p.id === chosen) ? chosen : "";
  const epicId = pickedEpicId ?? defaults.epicId;
  const projectPlaceholder =
    activeProjectsList.length === 0
      ? projects.length
        ? "No active projects"
        : "No projects"
      : "Select project…";
  const projectEpics = epics && epics.projectId === projectId ? epics.epics : [];
  const dialogTitleId = useId();
  const projectFieldId = useId();
  const epicFieldId = useId();
  const titleFieldId = useId();
  const descriptionFieldId = useId();
  const priorityFieldId = useId();
  const dueDateFieldId = useId();
  const labelsFieldId = useId();

  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    api.epics
      .list(projectId)
      .then((list) => {
        if (!cancelled) setEpics({ projectId, epics: list?.epics ?? [] });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [projectId]);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  // The ticket has nowhere to keep an image until it exists: pasting or
  // dropping one says so, and leaves the text as it was.
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const pasteImages = usePasteImages({
    owner: null,
    documents: null,
    value: description,
    onChange: setDescription,
    textareaRef: descriptionRef,
  });
  const [priority, setPriority] = useState("medium");
  const [dueDate, setDueDate] = useState("");
  const [labels, setLabels] = useState<string[]>([]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || !projectId) return;
    onCreate({
      projectId,
      title,
      description,
      priority,
      status: defaultStatus || DEFAULT_STATUS,
      dueDate: dueDate || undefined,
      labels,
      epic: epicId || undefined,
    });
    setLabels([]);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={dialogTitleId}
        className="w-full max-w-lg bg-slate-900 border border-slate-700 rounded-xl p-6"
      >
        <form onSubmit={handleSubmit} className="space-y-5">
          <div className="flex items-center justify-between">
            <h2 id={dialogTitleId} className="text-lg font-semibold text-white">New Ticket</h2>
            <button
              type="button"
              onClick={onClose}
              className="text-slate-500 hover:text-slate-300 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>

          <div className="space-y-4">
            <div>
              <label htmlFor={projectFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                Project
              </label>
              <select
                id={projectFieldId}
                value={projectId}
                onChange={(e) => {
                  setChosenProjectId(e.target.value);
                  setPickedEpicId(null);
                }}
                required
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
              >
                {projectId === "" && (
                  <option value="" disabled>
                    {projectPlaceholder}
                  </option>
                )}
                {activeProjectsList.map((p) => (
                  <option key={p.id} value={p.id}>
                    {projectLabel(p)}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label htmlFor={epicFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                Epic
              </label>
              <select
                id={epicFieldId}
                value={epicId}
                onChange={(e) => setPickedEpicId(e.target.value)}
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
              >
                <option value="">No epic</option>
                {[...projectEpics]
                  .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }))
                  .map((e) => (
                    <option key={e.id} value={e.id}>
                      {e.name}
                    </option>
                  ))}
              </select>
            </div>

            <div>
              <label htmlFor={titleFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                Title
              </label>
              <input
                id={titleFieldId}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="What needs to be done?"
                required
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>

            <div>
              <label htmlFor={descriptionFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                Description
              </label>
              <textarea
                ref={descriptionRef}
                id={descriptionFieldId}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                {...pasteImages.textareaProps}
                rows={3}
                placeholder="Add more detail…"
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500 resize-none"
              />
              <ImageUploadStatus
                uploads={pasteImages.uploads}
                problems={pasteImages.problems}
                onDismiss={pasteImages.dismissProblems}
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label htmlFor={priorityFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                  Priority
                </label>
                <select
                  id={priorityFieldId}
                  value={priority}
                  onChange={(e) => setPriority(e.target.value)}
                  className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500 capitalize"
                >
                  {PRIORITIES.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={dueDateFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                  Due Date
                </label>
                <input
                  id={dueDateFieldId}
                  type="date"
                  value={dueDate}
                  onChange={(e) => setDueDate(e.target.value)}
                  className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              </div>
            </div>

            <div>
              <label htmlFor={labelsFieldId} className="block text-xs font-medium text-slate-400 mb-1.5">
                Labels
              </label>
              <LabelPicker id={labelsFieldId} value={labels} onChange={setLabels} />
            </div>
          </div>

          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors"
            >
              Create Ticket
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
