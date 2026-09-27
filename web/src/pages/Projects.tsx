import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Plus, Trash2, X, FolderKanban, ChevronDown, ChevronUp, Bot } from "lucide-react";
import Markdown from "react-markdown";
import { useSearchParams } from "react-router-dom";
import { api, type Project } from "../api/client";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { useOverlayHistory } from "../hooks/useOverlayHistory";
import { latestSearchParams } from "../lib/latestSearch";
import ProjectTextFields from "../components/ProjectTextFields";
import LevelEntries from "../components/LevelEntries";
import DeleteProjectConfirm from "../components/DeleteProjectConfirm";

/** The query parameter naming the project whose dialog is open, by prefix or id. */
const PROJECT_PARAM = "project";

const DEFAULT_COLORS = [
  "#3b82f6",
  "#8b5cf6",
  "#ec4899",
  "#f97316",
  "#14b8a6",
  "#eab308",
  "#ef4444",
  "#06b6d4",
];

function ProjectModal({
  project,
  onClose,
  onSave,
}: {
  project?: Project;
  onClose: () => void;
  onSave: (data: Partial<Project>) => void;
}) {
  const isEdit = !!project;
  const titleId = useId();
  // Opened from a link to the project's entries, the narrow layout's body
  // scrolls to them; the wide one shows them beside the form already.
  const bodyRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState(project?.name || "");
  const [prefix, setPrefix] = useState(project?.prefix || "");
  const [description, setDescription] = useState(project?.description || "");
  // The list doesn't carry agent instructions, so an edited project's are
  // fetched when the form opens. Until they arrive (or if they can't be) the
  // field is read-only. Save sends them only when they were changed from what
  // was loaded, so saving the form for something else never overwrites a
  // newer value an agent wrote meanwhile.
  const [agentInstructions, setAgentInstructions] = useState("");
  const [loadedInstructions, setLoadedInstructions] = useState("");
  const [instructionsState, setInstructionsState] = useState<"ready" | "loading" | "error">(
    project ? "loading" : "ready",
  );
  const projectId = project?.id;
  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    api.projects.get(projectId).then(
      (full) => {
        if (cancelled) return;
        setAgentInstructions(full.agentInstructions ?? "");
        setLoadedInstructions(full.agentInstructions ?? "");
        setInstructionsState("ready");
      },
      () => {
        if (!cancelled) setInstructionsState("error");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [projectId]);
  const [icon, setIcon] = useState(project?.icon || "📋");
  const [color, setColor] = useState(project?.color || DEFAULT_COLORS[0]);

  // The store trims instructions, so a change of whitespace alone is no change.
  const instructionsChanged =
    instructionsState === "ready" && agentInstructions.trim() !== loadedInstructions.trim();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !prefix.trim()) return;
    onSave({
      name,
      prefix: prefix.toUpperCase(),
      description,
      ...(instructionsChanged ? { agentInstructions } : {}),
      icon,
      color,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      {/* Editing a project adds its entries: a column beside the form on a
          wide screen, where each scrolls on its own, and under the form on a
          narrow one, where the whole dialog scrolls. */}
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`w-full ${isEdit ? "max-w-6xl" : "max-w-2xl"} max-h-[90vh] flex flex-col overflow-hidden bg-slate-900 border border-slate-700 rounded-xl`}
      >
        <div className="flex shrink-0 items-center justify-between px-6 pt-6 pb-5">
          <h2 id={titleId} className="text-lg font-semibold text-white">
            {isEdit ? "Edit Project" : "New Project"}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="text-slate-500 hover:text-slate-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div
          ref={bodyRef}
          data-testid="project-dialog-body"
          className={`min-h-0 flex-1 overflow-y-auto ${
            isEdit ? "lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] lg:overflow-hidden" : ""
          }`}
        >
          <form
            onSubmit={handleSubmit}
            aria-labelledby={titleId}
            className="px-6 pb-6 space-y-5 lg:min-h-0 lg:overflow-y-auto"
          >
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-slate-400 mb-1.5">
                  Name
                </label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="My Project"
                  required
                  className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1.5">
                    Prefix
                  </label>
                  <input
                    value={prefix}
                    onChange={(e) => setPrefix(e.target.value.toUpperCase())}
                    placeholder="PRJ"
                    maxLength={5}
                    required
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1.5">
                    Icon
                  </label>
                  <input
                    value={icon}
                    onChange={(e) => setIcon(e.target.value)}
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500 text-center text-lg"
                  />
                </div>
              </div>

              <ProjectTextFields
                description={description}
                onDescriptionChange={setDescription}
                instructions={agentInstructions}
                onInstructionsChange={setAgentInstructions}
                instructionsHaveText={
                  instructionsState === "ready" ? agentInstructions.trim() !== "" : !!project?.hasAgentInstructions
                }
                instructionsState={instructionsState}
              />

              <div>
                <label className="block text-xs font-medium text-slate-400 mb-1.5">
                  Color
                </label>
                <div className="flex gap-2">
                  {DEFAULT_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setColor(c)}
                      className={`w-8 h-8 rounded-lg transition-all ${
                        color === c
                          ? "ring-2 ring-white ring-offset-2 ring-offset-slate-900 scale-110"
                          : "hover:scale-105"
                      }`}
                      style={{ backgroundColor: c }}
                    />
                  ))}
                </div>
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
                {isEdit ? "Save Changes" : "Create Project"}
              </button>
            </div>
          </form>
          {project && (
            <div
              data-testid="project-entries-column"
              className="border-t border-slate-800 px-6 pt-5 pb-6 lg:min-h-0 lg:overflow-y-auto lg:border-t-0 lg:border-l lg:pt-0"
            >
              <LevelEntries owner={{ projectId: project.id }} scrollParent={bodyRef} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// A project card's description, clamped to four lines. "Show more" appears
// only when the clamp cuts something off, measured again whenever the text's
// box changes size (the card narrowing or widening, an image loading) or the
// text changes. Once expanded nothing is clamped, so "Show less" stays until
// it is pressed and the collapsed description is measured afresh.
function ProjectDescription({
  text,
  expanded,
  onToggle,
}: {
  text: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [cutOff, setCutOff] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el || expanded) return;
    // The observer reports the box as soon as it starts watching, then on
    // every resize. A hidden line is a whole line high, so a pixel of slack
    // absorbs rounding without missing one.
    const observer = new ResizeObserver(() => {
      setCutOff(el.scrollHeight - el.clientHeight > 1);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [expanded, text]);

  return (
    <div className="mt-1">
      <div ref={ref} className={`prose-card overflow-hidden ${expanded ? "" : "line-clamp-4"}`}>
        <Markdown>{text}</Markdown>
      </div>
      {(expanded || cutOff) && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            onToggle();
          }}
          className="mt-1 inline-flex items-center gap-0.5 text-xs text-slate-500 hover:text-slate-300 transition-colors"
        >
          {expanded ? (
            <>
              Show less <ChevronUp className="w-3 h-3" />
            </>
          ) : (
            <>
              Show more <ChevronDown className="w-3 h-3" />
            </>
          )}
        </button>
      )}
    </div>
  );
}

/** The project a `?project=` names: by prefix, in any letter case, or by id. */
function findProject(projects: readonly Project[], ref: string): Project | undefined {
  const upper = ref.toUpperCase();
  return projects.find((p) => p.prefix.toUpperCase() === upper) ?? projects.find((p) => p.id === ref);
}

export default function Projects() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [loading, setLoading] = useState(true);
  // Whether the last read of the list succeeded: a failed one empties the
  // list, which must not read as the open project being gone.
  const [listed, setListed] = useState(false);

  // The open project's dialog lives in the URL (?project=ACP, by prefix or
  // id), so a link opens it and Back closes it.
  const [params] = useSearchParams();
  const overlays = useOverlayHistory();
  const ref = params.get(PROJECT_PARAM) ?? "";
  const found = ref ? findProject(projects, ref) : undefined;
  // The project the URL last found, with the parameter that found it. It is
  // followed by id, so a prefix renamed elsewhere keeps the dialog (and what
  // was typed in it) open, and it stays on screen while a read fails.
  const [held, setHeld] = useState<{ ref: string; project: Project } | null>(null);
  const heldHere = held && held.ref === ref ? held.project : null;
  const byId = !found && heldHere ? projects.find((p) => p.id === heldHere.id) : undefined;
  const current = found ?? byId ?? null;
  if (current && (heldHere !== current || held?.ref !== ref)) setHeld({ ref, project: current });
  if (!ref && held) setHeld(null);
  const editProject = current ?? (!listed ? heldHere : null);

  // A renamed prefix: the URL names the new one.
  const renamedTo = byId?.prefix;
  useEffect(() => {
    if (!renamedTo) return;
    const next = latestSearchParams(params);
    next.set(PROJECT_PARAM, renamedTo);
    overlays.replace(next);
  }, [renamedTo, params, overlays]);

  // Only a list read that succeeded and lacks the project (a bad link, or
  // deleted) drops the parameter.
  const missing = ref !== "" && !loading && listed && current === null;
  useEffect(() => {
    if (!missing) return;
    const next = latestSearchParams(params);
    next.delete(PROJECT_PARAM);
    overlays.replace(next);
  }, [missing, params, overlays]);
  const openProject = (project: Project) => {
    const next = latestSearchParams(params);
    next.set(PROJECT_PARAM, project.prefix);
    overlays.push(next);
  };
  const closeProject = () => overlays.closeAll();
  const [expandedDescs, setExpandedDescs] = useState<Set<string>>(new Set());

  const toggleDesc = (id: string) => {
    setExpandedDescs((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const load = useCallback(async () => {
    try {
      const data = await api.projects.list();
      setProjects(data || []);
      setListed(true);
    } catch {
      setProjects([]);
      setListed(false);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // The expanded descriptions and an open modal are state of their own, so a
  // live update just replaces the list.
  useLiveRefresh(load);

  const handleCreate = async (data: Partial<Project>) => {
    await api.projects.create(data);
    setShowCreate(false);
    load();
  };

  const handleUpdate = async (data: Partial<Project>) => {
    if (!editProject) return;
    await api.projects.update(editProject.id, data);
    closeProject();
    load();
  };

  // The project the confirm dialog asks about, and the trash button that
  // opened it, which gets focus back when the dialog is cancelled.
  const [deleting, setDeleting] = useState<Project | null>(null);
  const deleteTrigger = useRef<HTMLButtonElement | null>(null);
  const cancelDelete = () => {
    setDeleting(null);
    deleteTrigger.current?.focus();
    deleteTrigger.current = null;
  };
  const handleDeleted = () => {
    setDeleting(null);
    deleteTrigger.current = null;
    load();
  };

  return (
    <div className="h-full flex flex-col">
      <header className="shrink-0 flex items-center justify-between px-6 h-14 border-b border-slate-800">
        <h1 className="text-lg font-semibold text-white">Projects</h1>
        <button
          onClick={() => setShowCreate(true)}
          className="inline-flex items-center gap-2 px-3.5 py-1.5 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Project
        </button>
      </header>

      <div className="flex-1 overflow-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center h-64 text-slate-600">
            Loading projects…
          </div>
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-slate-600 space-y-3">
            <FolderKanban className="w-10 h-10 text-slate-700" />
            <p className="text-sm">No projects yet</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {projects.map((project) => (
              <div
                key={project.id}
                onClick={() => openProject(project)}
                className="relative bg-slate-900 border border-slate-700/50 hover:border-slate-600 rounded-xl p-5 transition-colors cursor-pointer"
              >
                <div
                  className="absolute inset-x-0 top-0 h-1 rounded-t-xl"
                  style={{ backgroundColor: project.color }}
                />
                <div className="flex items-start justify-between">
                  {project.icon && (
                    <span data-testid="project-icon" className="text-2xl">
                      {project.icon}
                    </span>
                  )}
                  {/* Faint at rest but always there and in the tab order, so
                      a keyboard can reach it; red, with a ring, under the
                      pointer or on keyboard focus. It only asks: the confirm
                      dialog deletes. */}
                  <button
                    type="button"
                    aria-label={`Delete project ${project.name}`}
                    title="Delete project"
                    onClick={(e) => {
                      e.stopPropagation();
                      deleteTrigger.current = e.currentTarget;
                      setDeleting(project);
                    }}
                    className="inline-flex rounded p-0.5 text-slate-600 opacity-45 transition-all hover:text-red-400 hover:opacity-100 hover:ring-2 hover:ring-blue-500 focus-visible:text-red-400 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                  >
                    <Trash2 aria-hidden="true" className="w-4 h-4" />
                  </button>
                </div>
                <h3 className="mt-3 text-sm font-semibold text-white">
                  {project.name}
                </h3>
                {project.description && (
                  <ProjectDescription
                    text={project.description}
                    expanded={expandedDescs.has(project.id)}
                    onToggle={() => toggleDesc(project.id)}
                  />
                )}
                {/* On a narrow card the row wraps and the marker takes its own line. */}
                <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="text-xs font-mono text-slate-500">
                    {project.prefix}
                  </span>
                  <span
                    className="inline-block w-1.5 h-1.5 shrink-0 rounded-full"
                    style={{ backgroundColor: project.color }}
                  />
                  {project.hasAgentInstructions && (
                    <span
                      data-testid="project-agent-instructions"
                      title="Has agent instructions"
                      className="ml-auto inline-flex shrink-0 items-center gap-1 text-xs text-slate-500"
                    >
                      <Bot aria-hidden="true" className="h-3 w-3" />
                      Agent instructions
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showCreate && (
        <ProjectModal
          onClose={() => setShowCreate(false)}
          onSave={handleCreate}
        />
      )}

      {editProject && (
        <ProjectModal
          key={editProject.id}
          project={editProject}
          onClose={closeProject}
          onSave={handleUpdate}
        />
      )}

      {deleting && (
        <DeleteProjectConfirm project={deleting} onCancel={cancelDelete} onDeleted={handleDeleted} />
      )}
    </div>
  );
}
