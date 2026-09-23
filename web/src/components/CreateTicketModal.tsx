import { useEffect, useId, useState } from "react";
import { X } from "lucide-react";
import { api, type TicketWrite, type Project } from "../api/client";
import LabelPicker from "./LabelPicker";
import { DEFAULT_STATUS } from "../lib/status";
import type { Filters } from "../lib/filters";
import { newTicketDefaults, type ProjectEpics } from "../lib/newTicketDefaults";

const PRIORITIES = ["urgent", "high", "medium", "low"];

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
  // Null until the user picks a project. Until then the form follows the
  // view's, so projects that load after the form opens still preselect it.
  const [pickedProjectId, setPickedProjectId] = useState<string | null>(null);
  // The epics of the project the form is on, tagged with it so a list for the
  // project just left is never offered for the new one.
  const [epics, setEpics] = useState<ProjectEpics | null>(null);
  // Null until the user picks an epic, and again when they pick another
  // project. Until then the form follows the view's epic filter, once the
  // project's epics have loaded, as long as it is on the view's project.
  const [pickedEpicId, setPickedEpicId] = useState<string | null>(null);
  const defaults = newTicketDefaults(filters, projects, epics);
  const projectId = pickedProjectId ?? defaults.projectId;
  const epicId = pickedEpicId ?? defaults.epicId;
  const projectEpics = epics && epics.projectId === projectId ? epics.epics : [];
  const epicFieldId = useId();

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
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-lg bg-slate-900 border border-slate-700 rounded-xl p-6 space-y-5"
      >
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">New Ticket</h2>
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
            <label className="block text-xs font-medium text-slate-400 mb-1.5">
              Project
            </label>
            <select
              value={projectId}
              onChange={(e) => {
                setPickedProjectId(e.target.value);
                setPickedEpicId(null);
              }}
              required
              className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
            >
              <option value="">Select project…</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.icon} {p.name}
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
            <label className="block text-xs font-medium text-slate-400 mb-1.5">
              Title
            </label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="What needs to be done?"
              required
              className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1.5">
              Description
            </label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder="Add more detail…"
              className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500 resize-none"
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-medium text-slate-400 mb-1.5">
                Priority
              </label>
              <select
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
              <label className="block text-xs font-medium text-slate-400 mb-1.5">
                Due Date
              </label>
              <input
                type="date"
                value={dueDate}
                onChange={(e) => setDueDate(e.target.value)}
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1.5">Labels</label>
            <LabelPicker value={labels} onChange={setLabels} />
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
  );
}
