import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, CircleDashed, Ellipsis, Layers, Paperclip, Plus, X } from "lucide-react";
import { api, type Epic, type EpicList, type EpicProgress, type Project } from "../api/client";
import EpicForm from "../components/EpicForm";
import EpicModal from "../components/EpicModal";
import ProjectSelect from "../components/ProjectSelect";
import { useEpicParam } from "../hooks/useEpicParam";
import { useFilters } from "../hooks/useFilters";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { awaitingProject, namedProject, type ActivityTicket } from "../lib/defaultProject";
import {
  activeCount,
  arrangeEpics,
  barSegments,
  deleteMessage,
  epicLink,
  progressText,
  serverMessage,
  showsNoEpic,
} from "../lib/epics";
import { NO_EPIC } from "../lib/filters";
import { readLastView } from "../lib/lastView";
import { STATUS_COLORS, STATUS_LABELS } from "../lib/status";

// The progress bar: one segment per status, as wide as its share of the
// tickets, in the status colours. An epic with no tickets has an empty bar.
function ProgressBar({ progress }: { progress: EpicProgress }) {
  const segments = barSegments(progress);
  const label =
    segments.length === 0
      ? "No tickets"
      : segments.map((s) => `${s.count} ${STATUS_LABELS[s.status].toLowerCase()}`).join(", ");
  return (
    <div
      role="img"
      aria-label={label}
      data-testid="epic-bar"
      className="flex h-1.5 w-full overflow-hidden rounded-full bg-slate-800"
    >
      {segments.map((s) => (
        <div
          key={s.status}
          data-status={s.status}
          className={STATUS_COLORS[s.status]}
          style={{ width: `${s.percent}%` }}
        />
      ))}
    </div>
  );
}

// The ⋯ menu on an epic's row: a disclosure, the ⋯ button showing or hiding
// two plain buttons. A click outside, focus moving outside, or Escape hides
// them, and Escape puts focus back on ⋯. Each action is handed ⋯ so a dialog it opens can give
// focus back to it.
function RowMenu({
  name,
  onEdit,
  onDelete,
}: {
  name: string;
  onEdit: (opener: HTMLElement | null) => void;
  onDelete: (opener: HTMLElement | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const escape = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      buttonRef.current?.focus();
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);

  const pick = (action: (opener: HTMLElement | null) => void) => () => {
    setOpen(false);
    action(buttonRef.current);
  };

  return (
    <div
      ref={ref}
      className="relative shrink-0"
      onBlur={(e) => {
        // Focus moving to something outside (Tab away, or another row's ⋯)
        // hides the panel. A blur to nowhere, as a click in some browsers
        // gives, is left to the click-outside handler above.
        const next = e.relatedTarget as Node | null;
        if (next && !e.currentTarget.contains(next)) setOpen(false);
      }}
    >
      <button
        ref={buttonRef}
        type="button"
        aria-label={`Actions for ${name}`}
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        onClick={() => setOpen((o) => !o)}
        className="rounded p-1 text-slate-500 transition-colors hover:bg-slate-800 hover:text-slate-300"
      >
        <Ellipsis className="h-4 w-4" />
      </button>
      {open && (
        <div
          id={panelId}
          className="absolute right-0 top-full z-10 mt-1 w-32 overflow-hidden rounded-lg border border-slate-700 bg-slate-900 py-1 shadow-xl"
        >
          <button
            type="button"
            onClick={pick(onEdit)}
            className="block w-full px-3 py-1.5 text-left text-sm text-slate-300 hover:bg-slate-800 focus:bg-slate-800 focus:outline-none"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={pick(onDelete)}
            className="block w-full px-3 py-1.5 text-left text-sm text-red-400 hover:bg-slate-800 focus:bg-slate-800 focus:outline-none"
          >
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

// One row: an epic, or the project's tickets without one. The row opens the
// tickets it counts; the documents button and the menu, when there are any,
// sit outside that link.
function Row({
  name,
  description,
  progress,
  href,
  noEpic,
  documents,
  menu,
}: {
  name: string;
  description?: string;
  progress: EpicProgress;
  href: string;
  noEpic?: boolean;
  documents?: React.ReactNode;
  menu?: React.ReactNode;
}) {
  const active = noEpic ? 0 : activeCount(progress);
  const Icon = noEpic ? CircleDashed : Layers;
  return (
    <li data-testid="epic-row" className="flex items-center gap-2 pr-3 hover:bg-slate-800/40">
      <Link to={href} className="flex min-w-0 flex-1 items-center gap-4 py-3 pl-4">
        <Icon className={`h-4 w-4 shrink-0 ${noEpic ? "text-slate-600" : "text-slate-500"}`} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span
              data-testid="epic-name"
              className={`truncate text-sm font-medium ${noEpic ? "text-slate-400" : "text-slate-200"}`}
            >
              {name}
            </span>
            {active > 0 && (
              <span className="shrink-0 rounded bg-blue-500/10 px-1.5 py-0.5 text-[11px] font-medium text-blue-400">
                {active} active
              </span>
            )}
          </div>
          {description && <p className="mt-0.5 truncate text-xs text-slate-500">{description}</p>}
        </div>
        <div className="w-40 shrink-0 sm:w-56">
          <ProgressBar progress={progress} />
        </div>
        <span data-testid="epic-count" className="w-24 shrink-0 text-right text-xs tabular-nums text-slate-500">
          {progressText(progress)}
        </span>
      </Link>
      {documents}
      <div className="w-6 shrink-0">{menu}</div>
    </li>
  );
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// A dialog's shell: the scrim, the panel and its title, closed by Escape.
// It is modal, so Tab and Shift+Tab stay inside it. The page gives focus back
// to the button that opened it once it has closed.
function Dialog({
  title,
  role = "dialog",
  describedBy,
  onClose,
  children,
}: {
  title: string;
  role?: "dialog" | "alertdialog";
  describedBy?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = panelRef.current;
    if (e.key !== "Tab" || !root) return;
    const focusable = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (!root.contains(active)) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
    } else if (e.shiftKey && (active === first || active === root)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm"
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
    >
      <div
        ref={panelRef}
        role={role}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={describedBy}
        tabIndex={-1}
        onKeyDown={trapTab}
        className="w-full max-w-md rounded-xl border border-slate-700 bg-slate-900 p-6 shadow-2xl focus:outline-none"
      >
        <div className="mb-5 flex items-center justify-between">
          <h2 id={titleId} className="text-lg font-semibold text-white">
            {title}
          </h2>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="text-slate-500 transition-colors hover:text-slate-300"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

/** New epic: the epic form in a small dialog. Editing is the epic modal's. */
function EpicDialog({
  epics,
  onClose,
  onSave,
}: {
  /** The project's epics, which the name must differ from. */
  epics: readonly Epic[];
  onClose: () => void;
  onSave: (data: { name: string; description: string }) => Promise<void>;
}) {
  return (
    <Dialog title="New epic" onClose={onClose}>
      <EpicForm epics={epics} onCancel={onClose} onSave={onSave} />
    </Dialog>
  );
}

function DeleteDialog({
  epic,
  onClose,
  onDelete,
}: {
  epic: Epic;
  onClose: () => void;
  onDelete: () => Promise<void>;
}) {
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const bodyId = useId();

  useEffect(() => cancelRef.current?.focus(), []);

  const confirm = async () => {
    if (deleting) return;
    setDeleting(true);
    try {
      await onDelete();
    } catch (err) {
      setError(serverMessage(err, "The epic was not deleted."));
      setDeleting(false);
    }
  };

  return (
    <Dialog
      title={`Delete "${epic.name}"?`}
      role="alertdialog"
      describedBy={bodyId}
      onClose={onClose}
    >
      <p id={bodyId} className="text-sm text-slate-400">
        {deleteMessage(epic.total, epic.documentCount ?? 0)}
      </p>
      {error && (
        <p role="alert" className="mt-3 text-xs text-red-400">
          {error}
        </p>
      )}
      <div className="mt-6 flex justify-end gap-2">
        <button
          ref={cancelRef}
          type="button"
          onClick={onClose}
          className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 transition-colors hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={confirm}
          className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400"
        >
          Delete
        </button>
      </div>
    </Dialog>
  );
}

type Editing = { kind: "new" } | { kind: "delete"; epic: Epic } | null;

/**
 * The Epics view: the selected project's epics, each with its progress, the
 * busy ones first. A row opens its tickets in the ticket view used last; its
 * paperclip, and its menu's Edit, open the epic modal, which the URL names
 * (`epic=<name>`, see useEpicParam).
 */
export default function Epics() {
  const filterState = useFilters();
  const { filters } = filterState;
  // Null until the first load, and after a failed first load, none.
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [tickets, setTickets] = useState<ActivityTicket[] | null>(null);
  // The epics loaded, and the project they are for, so a list for the
  // project shown before never shows under the next one.
  const [loaded, setLoaded] = useState<{ project: string; list: EpicList | null } | null>(null);
  const [completeOpen, setCompleteOpen] = useState(false);
  const [editing, setEditing] = useState<Editing>(null);
  // Where focus goes when a dialog closes: the button that opened it.
  const openerRef = useRef<HTMLElement | null>(null);
  const newEpicRef = useRef<HTMLButtonElement>(null);
  const openDialog = (next: Editing, opener: HTMLElement | null) => {
    openerRef.current = opener;
    setEditing(next);
  };
  // Focus goes back once a dialog has closed and gone, from here rather than
  // from the dialog's own effect cleanup: StrictMode runs that cleanup once
  // as the dialog mounts in development, which would pull focus straight
  // back out of it.
  const dialogWasOpen = useRef(false);
  useEffect(() => {
    if (editing) {
      dialogWasOpen.current = true;
      return;
    }
    if (!dialogWasOpen.current) return;
    dialogWasOpen.current = false;
    const target = openerRef.current;
    if (target?.isConnected) target.focus();
  }, [editing]);

  const shownProject = projects ? namedProject(projects.map((p) => p.prefix), filters.project) : null;

  // Each load takes only its newest reply, and a failed reload keeps what
  // is on screen; only a failed first load settles on nothing.
  const projectSeq = useRef(0);
  const loadProjects = useCallback(() => {
    const seq = ++projectSeq.current;
    api.projects
      .list()
      .then((p) => seq === projectSeq.current && setProjects(p ?? []))
      .catch(() => seq === projectSeq.current && setProjects((prev) => prev ?? []));
  }, []);

  // Every project's tickets, only for when each project last changed, which
  // the pick of a project for a URL without one reads.
  const ticketSeq = useRef(0);
  const loadTickets = useCallback(() => {
    const seq = ++ticketSeq.current;
    api.tickets
      .list()
      .then((t) => seq === ticketSeq.current && setTickets(t ?? []))
      .catch(() => seq === ticketSeq.current && setTickets((prev) => prev ?? []));
  }, []);

  const epicSeq = useRef(0);
  const loadEpics = useCallback((project: string): Promise<void> => {
    const seq = ++epicSeq.current;
    return api.epics
      .list(project)
      .then((list) => seq === epicSeq.current && setLoaded({ project, list }))
      .then(
        () => {},
        () => {
          if (seq === epicSeq.current) setLoaded((prev) => (prev?.project === project ? prev : { project, list: null }));
        },
      );
  }, []);

  useEffect(() => {
    loadProjects();
    loadTickets();
  }, [loadProjects, loadTickets]);

  useEffect(() => {
    if (shownProject) loadEpics(shownProject);
  }, [shownProject, loadEpics]);

  // A live update refreshes the list and leaves an open dialog, and what was
  // typed into it, alone; the dialog's checks then run against the new list.
  const shownRef = useRef(shownProject);
  useEffect(() => {
    shownRef.current = shownProject;
  }, [shownProject]);
  const refresh = useCallback(() => {
    loadProjects();
    loadTickets();
    if (shownRef.current) loadEpics(shownRef.current);
  }, [loadProjects, loadTickets, loadEpics]);
  useLiveRefresh(refresh);

  const current = shownProject && loaded?.project === shownProject ? loaded : null;
  const epics = useMemo(() => current?.list?.epics ?? [], [current]);
  const { live, complete } = useMemo(() => arrangeEpics(epics), [epics]);
  const noEpic = current?.list?.noEpic;
  const waiting = awaitingProject(filters.project, projects) || (shownProject !== null && current === null);
  const view = readLastView();
  const link = (epic: string) => epicLink(view, shownProject ?? "", epic);

  // The open epic modal comes from the URL (`epic=<name>`), so it survives a
  // reload and Back closes it. Only the shown project's loaded list can name
  // one, so a list for another project never opens or drops anything.
  const epicParam = useEpicParam(current?.list ? epics : null);
  // Focus goes back to what opened the modal once it has closed, as it does
  // for the dialogs.
  const modalOpenerRef = useRef<HTMLElement | null>(null);
  const modalWasOpen = useRef(false);
  useEffect(() => {
    if (epicParam.selected) {
      modalWasOpen.current = true;
      return;
    }
    if (!modalWasOpen.current) return;
    modalWasOpen.current = false;
    if (modalOpenerRef.current?.isConnected) modalOpenerRef.current.focus();
  }, [epicParam.selected]);
  const openEpic = (epic: Epic, opener: HTMLElement | null) => {
    modalOpenerRef.current = opener;
    epicParam.open(epic);
  };

  const close = () => setEditing(null);
  const saved = () => {
    setEditing(null);
    if (shownProject) loadEpics(shownProject);
  };

  const epicRow = (epic: Epic) => (
    <Row
      key={epic.id}
      name={epic.name}
      description={epic.description}
      progress={epic}
      href={link(epic.name)}
      documents={
        <button
          type="button"
          aria-label={`Documents of ${epic.name} (${epic.documentCount ?? 0})`}
          title="Documents"
          onClick={(e) => openEpic(epic, e.currentTarget)}
          className={`inline-flex shrink-0 items-center gap-1 rounded-md border border-slate-800 px-1.5 py-0.5 text-xs tabular-nums transition-colors hover:bg-slate-800 hover:text-slate-200 ${
            (epic.documentCount ?? 0) > 0 ? "text-slate-300" : "text-slate-600"
          }`}
        >
          <Paperclip aria-hidden="true" className="h-3 w-3" />
          {epic.documentCount ?? 0}
        </button>
      }
      menu={
        <RowMenu
          name={epic.name}
          onEdit={(opener) => openEpic(epic, opener)}
          onDelete={(opener) => openDialog({ kind: "delete", epic }, opener)}
        />
      }
    />
  );

  let body: React.ReactNode;
  if (waiting) {
    body = <div className="flex h-64 items-center justify-center text-slate-600">Loading epics…</div>;
  } else if (!shownProject) {
    body = <p className="px-4 py-6 text-sm text-slate-500">No project to show epics for.</p>;
  } else if (current?.list === null) {
    body = <p className="px-4 py-6 text-sm text-slate-500">The epics could not be loaded.</p>;
  } else {
    body = (
      <div className="space-y-4">
        <ul className="divide-y divide-slate-800 rounded-lg border border-slate-800 bg-slate-900">
          {live.map(epicRow)}
          {showsNoEpic(noEpic) && noEpic && (
            <Row key="no-epic" name="No epic" progress={noEpic} href={link(NO_EPIC)} noEpic />
          )}
          {live.length === 0 && !showsNoEpic(noEpic) && (
            <li className="px-4 py-6 text-sm text-slate-500">
              {complete.length > 0 ? "Every epic is complete." : "No epics yet."}
            </li>
          )}
        </ul>
        {complete.length > 0 && (
          <section>
            <button
              type="button"
              aria-expanded={completeOpen}
              onClick={() => setCompleteOpen((open) => !open)}
              className="inline-flex items-center gap-1 px-1 py-1 text-xs font-medium text-slate-400 transition-colors hover:text-slate-200"
            >
              <ChevronRight className={`h-3.5 w-3.5 transition-transform ${completeOpen ? "rotate-90" : ""}`} />
              Complete ({complete.length})
            </button>
            {completeOpen && (
              <ul
                aria-label="Complete epics"
                className="mt-2 divide-y divide-slate-800 rounded-lg border border-slate-800 bg-slate-900"
              >
                {complete.map(epicRow)}
              </ul>
            )}
          </section>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-slate-800 px-6">
        <h1 className="text-lg font-semibold text-white">Epics</h1>
        <button
          ref={newEpicRef}
          type="button"
          disabled={!shownProject || current === null}
          onClick={(e) => openDialog({ kind: "new" }, e.currentTarget)}
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Plus className="h-4 w-4" />
          New epic
        </button>
      </header>

      <div className="flex shrink-0 items-center gap-2 border-b border-slate-800/50 px-6 py-3">
        <ProjectSelect state={filterState} projects={projects} tickets={tickets} />
      </div>

      <div className="flex-1 overflow-auto p-6">
        <div className="max-w-4xl">{body}</div>
      </div>

      {editing?.kind === "new" && shownProject && (
        <EpicDialog
          epics={epics}
          onClose={close}
          onSave={async (data) => {
            await api.epics.create({ projectId: shownProject, ...data });
            saved();
          }}
        />
      )}
      {epicParam.selected && shownProject && (
        <EpicModal
          key={epicParam.selected.id}
          epic={epicParam.selected}
          epics={epics}
          projectPrefix={shownProject}
          closeRequested={epicParam.closeRequested}
          onCloseCancelled={epicParam.cancelClose}
          onDirtyChange={epicParam.onDirtyChange}
          onClose={epicParam.close}
          onSaved={async (updated) => {
            // Save closes the modal, as the Edit epic dialog did. The URL
            // names the new name first, so Forward reopens it.
            await loadEpics(shownProject);
            epicParam.renamed(updated);
            epicParam.close();
          }}
        />
      )}
      {editing?.kind === "delete" && (
        <DeleteDialog
          epic={editing.epic}
          onClose={close}
          onDelete={async () => {
            await api.epics.delete(editing.epic.id);
            // The deleted epic's row, and its ⋯, are about to go.
            openerRef.current = newEpicRef.current;
            saved();
          }}
        />
      )}
    </div>
  );
}
