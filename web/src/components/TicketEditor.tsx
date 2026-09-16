import { useCallback, useEffect, useId, useRef, useState } from "react";
import { X, Trash2, CheckCircle2, Circle, Pencil, Eye, Copy, Check, RefreshCw, AlertTriangle } from "lucide-react";
import Markdown from "react-markdown";
import { api, type Ticket, type Project, type Subtask, type TicketWrite } from "../api/client";
import LabelPicker from "./LabelPicker";
import RepoPicker from "./RepoPicker";
import DependencyPicker from "./DependencyPicker";
import { saveErrorMessage } from "../lib/saveError";
import { STATUSES, STATUS_LABELS, STATUS_STYLES, isStatus } from "../lib/status";
import {
  changedFields,
  editedWrite,
  ticketFields,
  toDateInputValue,
  type TicketFields,
} from "../lib/ticketFields";

const PRIORITIES = ["urgent", "high", "medium", "low"];

const FIELD_LABEL = "block text-xs font-medium text-slate-500 mb-1.5";
const SECTION_HEADING = "text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5";
const SELECT =
  "w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// The ticket editor: a modal that fills the viewport minus a margin, so the
// scrim stays visible around it. It closes from the header's close button,
// from Escape, and from a click on the scrim; with unsaved edits each of those
// asks to discard them first. Wide windows get two columns (content on the
// left, fields on the right); narrow ones stack them.
export default function TicketEditor({
  ticket,
  projects,
  ticketUrl,
  closeRequested = false,
  onCloseCancelled,
  onDirtyChange,
  onClose,
  onUpdate,
  onDelete,
}: {
  ticket: Ticket;
  projects: Project[];
  // The ticket's shareable URL, from useTicketParam. Without one the header
  // shows no link or copy button.
  ticketUrl?: string;
  // A close asked for from outside the editor: the browser's Back button, or
  // anything else that drops the `ticket` parameter. It goes through the same
  // discard question as the close button, and cancelling it calls
  // onCloseCancelled, which puts the parameter back.
  closeRequested?: boolean;
  onCloseCancelled?: () => void;
  // Reports unsaved edits, so whoever owns the URL knows the editor cannot
  // just be unmounted.
  onDirtyChange?: (dirty: boolean) => void;
  onClose: () => void;
  // Answering with a promise lets the editor wait for the save and keep the
  // edits when it fails; every page does, since each one refetches after it.
  onUpdate: (id: string, data: TicketWrite) => void | Promise<void>;
  onDelete: (id: string) => void;
}) {
  const [title, setTitle] = useState(ticket.title);
  const [description, setDescription] = useState(ticket.description);
  const [status, setStatus] = useState(ticket.status);
  const [priority, setPriority] = useState(ticket.priority);
  const [dueDate, setDueDate] = useState(toDateInputValue(ticket.dueDate));
  const [repos, setRepos] = useState<string[]>(ticket.repos || []);
  const [labels, setLabels] = useState<string[]>((ticket.labels || []).map((l) => l.name));
  const [dependsOn, setDependsOn] = useState(ticket.dependsOn || []);
  const [subtasks, setSubtasks] = useState<Subtask[]>(ticket.subtasks || []);
  const [newSubtask, setNewSubtask] = useState("");
  const [dirty, setDirty] = useState(false);
  const [descMode, setDescMode] = useState<"preview" | "write">(description ? "preview" : "write");

  // List responses deliberately omit `blocks`, so the editor fetches the full
  // ticket itself. Until that resolves it renders the ticket it was handed, so
  // the editor still opens instantly.
  const [detail, setDetail] = useState<Ticket>(ticket);
  const dirtyRef = useRef(false);
  // The server version the controls were last filled from. Save sends what
  // has moved away from it, so a field the user never touched is left for
  // whoever else changed it.
  const baseRef = useRef<TicketFields>(ticketFields(ticket));
  // A newer version of this ticket that arrived while there were unsaved
  // edits, kept for the notice's reload rather than applied.
  const [changed, setChanged] = useState<Ticket | null>(null);
  const changedRef = useRef<Ticket | null>(null);
  // A save in flight, and what the last one failed with. Nothing is given up
  // until the save has come back: a failed one leaves the editor as it was,
  // edits and all, with a line saying why.
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const noteChanged = (latest: Ticket | null) => {
    changedRef.current = latest;
    setChanged(latest);
  };

  /** The fields as the controls hold them right now. */
  const currentFields = (): TicketFields => ({
    title,
    description,
    status,
    priority,
    dueDate,
    repos,
    labels,
    dependsOn: dependsOn.map((d) => d.id),
  });

  // Fills every control from a ticket and forgets the edits, which is what a
  // quiet refresh and the notice's reload both do.
  const syncFrom = useCallback((full: Ticket) => {
    setTitle(full.title);
    setDescription(full.description);
    setStatus(full.status);
    setPriority(full.priority);
    setDueDate(toDateInputValue(full.dueDate));
    setRepos(full.repos || []);
    setLabels((full.labels || []).map((l) => l.name));
    setDependsOn(full.dependsOn || []);
    setSubtasks(full.subtasks || []);
    baseRef.current = ticketFields(full);
    dirtyRef.current = false;
    setDirty(false);
    changedRef.current = null;
    setChanged(null);
    setSaveError(null);
  }, []);

  const dialogRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLDivElement>(null);
  // "Discard unsaved changes?" is up for one of two reasons: the editor itself
  // asked (the close button, Escape, the scrim), which is `asking`, or the URL
  // stopped naming this ticket while there were unsaved edits, which is
  // derived rather than stored — that request stands until whoever owns the
  // URL has acted on the answer. `pendingActionRef` is what the editor's own
  // question is waiting on, and `focusBeforeConfirmRef` the element to give
  // focus back to when it is cancelled.
  const [asking, setAsking] = useState(false);
  const urlAsking = closeRequested && dirty;
  const confirmOpen = asking || urlAsking;
  const pendingActionRef = useRef<(() => void) | null>(null);
  const focusBeforeConfirmRef = useRef<HTMLElement | null>(null);
  const ids = useId();
  const keyId = `${ids}-key`;
  const statusId = `${ids}-status`;
  const priorityId = `${ids}-priority`;
  const dueDateId = `${ids}-due`;

  // The full ticket, fetched on open and again whenever the ticket the editor
  // was handed changes — which is how an edit made elsewhere, arriving as a
  // live refresh with a newer `updatedAt`, reaches the editor.
  //
  // With nothing unsaved the editor simply refreshes, quietly: the user is
  // looking at whatever is true now. With unsaved edits nothing is replaced,
  // because a control filled in from the server under the user's hands is an
  // edit lost; the notice says the ticket changed and offers the new version
  // instead.
  useEffect(() => {
    let cancelled = false;
    api.tickets
      .get(ticket.id)
      .then((full) => {
        if (cancelled) return;
        setDetail(full);
        if (!dirtyRef.current) {
          syncFrom(full);
          return;
        }
        const latest = ticketFields(full);
        noteChanged(changedFields(baseRef.current, latest).length > 0 ? full : null);
      })
      .catch(() => {
        // A failed fetch just leaves what is on screen where it is.
      });
    return () => {
      cancelled = true;
    };
  }, [ticket.id, ticket.updatedAt, syncFrom]);

  // Move focus into the dialog on open and give it back on close.
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    return () => {
      if (previous && previous.isConnected) previous.focus();
    };
  }, []);

  // Runs an action that would throw away unsaved edits: straight away when
  // there are none, otherwise once the user confirms the discard. Every close
  // path inside the editor goes through here, and so can anything else that
  // drops edits. While the question is already on screen it does nothing, so
  // the pending action and the element to give focus back to stay as they were.
  const confirmDiscardThen = useCallback(
    (action: () => void) => {
      if (confirmOpen) return;
      if (!dirty) {
        action();
        return;
      }
      const active = document.activeElement;
      focusBeforeConfirmRef.current = active instanceof HTMLElement ? active : null;
      pendingActionRef.current = action;
      setAsking(true);
    },
    [dirty, confirmOpen],
  );

  const requestClose = useCallback(() => confirmDiscardThen(onClose), [confirmDiscardThen, onClose]);

  // The notice's reload: the latest version replaces the edits, so it asks
  // first, down the same path every other discard takes.
  const reloadChanged = () =>
    confirmDiscardThen(() => {
      const latest = changedRef.current;
      if (latest) syncFrom(latest);
    });

  // Tell whoever owns the URL about unsaved edits, so a Back that drops the
  // `ticket` parameter keeps the editor mounted long enough to ask about them
  // rather than unmounting it.
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  // A close the URL asked for with nothing left to lose. Normally the owner of
  // the URL has already dropped the editor by the time this could run, since it
  // knows the edits are gone; this covers the beat before that report lands,
  // say just after a save.
  //
  // onClose is read from a ref, the way useLiveRefresh reads its callback, so
  // the inline arrow every page passes does not re-run this on each render.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);
  useEffect(() => {
    if (closeRequested && !dirty) onCloseRef.current();
  }, [closeRequested, dirty]);

  const cancelDiscard = useCallback(() => {
    // Cancelling a close the URL asked for has to undo it as well, or the
    // editor would be left sitting on a URL that no longer names its ticket.
    const undoUrlClose = urlAsking;
    pendingActionRef.current = null;
    setAsking(false);
    if (undoUrlClose) onCloseCancelled?.();
    const previous = focusBeforeConfirmRef.current;
    const dialog = dialogRef.current;
    // Give focus back inside the editor once the confirm has gone.
    setTimeout(() => {
      if (previous && previous.isConnected && dialog?.contains(previous)) previous.focus();
      else dialog?.focus();
    });
  }, [urlAsking, onCloseCancelled]);

  const acceptDiscard = () => {
    // With no action of the editor's own waiting, the question came from the
    // URL and the answer is simply to go.
    const action = pendingActionRef.current ?? (urlAsking ? onClose : null);
    pendingActionRef.current = null;
    setAsking(false);
    action?.();
  };

  // Escape asks to close the editor, or cancels the discard confirm while it
  // is open. A control that handles Escape itself (and calls preventDefault)
  // keeps it.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || e.isComposing) return;
      e.preventDefault();
      if (confirmOpen) cancelDiscard();
      else requestClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [confirmOpen, cancelDiscard, requestClose]);

  // Keep Tab and Shift+Tab inside the dialog, or inside the confirm while it
  // is open.
  const handleTrapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = confirmOpen ? confirmRef.current : dialogRef.current;
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

  const markDirty = () => {
    dirtyRef.current = true;
    setDirty(true);
  };

  // Save sends the fields the user edited and no others, so a change made
  // elsewhere to a field they never touched survives; where both changed the
  // same field the user's value wins, since the notice already told them. A
  // due date the user cleared goes as "", the API's explicit clear, while one
  // they never touched is left out and so left alone.
  //
  // The edits are only let go once the save has come back. A save that fails —
  // the ticket deleted while the editor was open, the server refusing the
  // input, the network gone — leaves the editor dirty and open with every
  // field as the user left it, so pressing Save again sends the same fields.
  const handleSave = async () => {
    const current = currentFields();
    setSaveError(null);
    setSaving(true);
    try {
      await onUpdate(ticket.id, editedWrite(baseRef.current, current));
    } catch (error) {
      setSaveError(saveErrorMessage(error));
      return;
    } finally {
      setSaving(false);
    }
    baseRef.current = current;
    dirtyRef.current = false;
    setDirty(false);
    noteChanged(null);
  };

  const handleAddSubtask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newSubtask.trim()) return;
    const sub = await api.tickets.addSubtask(ticket.id, newSubtask);
    setSubtasks((prev) => [...prev, sub]);
    setNewSubtask("");
  };

  const handleToggleSubtask = async (id: string) => {
    const updated = await api.subtasks.toggle(id);
    setSubtasks((prev) => prev.map((s) => (s.id === id ? updated : s)));
  };

  const handleDeleteSubtask = async (id: string) => {
    await api.subtasks.delete(id);
    setSubtasks((prev) => prev.filter((s) => s.id !== id));
  };

  const ticketKey = `${ticket.projectPrefix}-${ticket.number}`;
  const statusStyle = isStatus(status) ? STATUS_STYLES[status] : "bg-slate-500/20 text-slate-400";
  const statusLabel = isStatus(status) ? STATUS_LABELS[status] : status.replace("_", " ");

  return (
    <div className="fixed inset-0 z-50 flex p-3 sm:p-6 lg:p-10">
      <div
        data-testid="ticket-editor-scrim"
        aria-hidden="true"
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={requestClose}
      />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={keyId}
        tabIndex={-1}
        onKeyDown={handleTrapTab}
        className="relative mx-auto flex h-full w-full max-w-[96rem] flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header
          inert={confirmOpen}
          className="flex shrink-0 items-center gap-2 border-b border-slate-800 px-4 py-3 sm:gap-3 sm:px-6"
        >
          <h2 id={keyId} className="shrink-0 whitespace-nowrap font-mono text-xs text-slate-400">
            {ticketKey}
          </h2>
          <span
            data-testid="ticket-editor-status"
            className={`shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium ${statusStyle}`}
          >
            {statusLabel}
          </span>
          <div className="min-w-0 flex-1">
            {ticketUrl && <TicketLink url={ticketUrl} />}
          </div>
          {dirty && (
            <button
              type="button"
              onClick={handleSave}
              disabled={saving}
              className="shrink-0 whitespace-nowrap rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-60"
            >
              Save Changes
            </button>
          )}
          <button
            type="button"
            aria-label="Delete ticket"
            title="Delete ticket"
            onClick={() => {
              onDelete(ticket.id);
              onClose();
            }}
            className="shrink-0 text-slate-500 transition-colors hover:text-red-400"
          >
            <Trash2 className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label="Close"
            title="Close"
            onClick={requestClose}
            className="shrink-0 text-slate-500 transition-colors hover:text-slate-300"
          >
            <X className="h-5 w-5" />
          </button>
        </header>

        {saveError && (
          <div
            inert={confirmOpen}
            role="alert"
            data-testid="ticket-save-error"
            className="flex shrink-0 items-center gap-2 border-b border-red-500/40 bg-red-500/10 px-4 py-2 text-xs text-red-200 sm:px-6"
          >
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            <span>{saveError}</span>
          </div>
        )}

        {changed && (
          <div
            inert={confirmOpen}
            role="status"
            data-testid="ticket-changed-notice"
            className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-200 sm:px-6"
          >
            <span>This ticket changed elsewhere. Your unsaved edits are kept.</span>
            <button
              type="button"
              onClick={reloadChanged}
              className="inline-flex items-center gap-1.5 rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 transition-colors hover:bg-amber-500/20"
            >
              <RefreshCw className="h-3 w-3" />
              Reload
            </button>
          </div>
        )}

        <div
          inert={confirmOpen}
          data-testid="ticket-editor-body"
          className="min-h-0 flex-1 overflow-y-auto lg:grid lg:grid-cols-[minmax(0,1fr)_22rem] lg:overflow-hidden xl:grid-cols-[minmax(0,1fr)_26rem]"
        >
          <section
            aria-label="Ticket content"
            className="space-y-6 p-4 sm:p-6 lg:overflow-y-auto"
          >
            {/* The open agent request block goes here, first in this column. */}
            <input
              aria-label="Title"
              value={title}
              onChange={(e) => {
                setTitle(e.target.value);
                markDirty();
              }}
              className="w-full bg-transparent text-xl font-semibold text-white focus:outline-none"
            />

            <div>
              <div className="mb-1.5 flex items-center gap-1">
                <button
                  type="button"
                  aria-pressed={descMode === "write"}
                  onClick={() => setDescMode("write")}
                  className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
                    descMode === "write"
                      ? "bg-slate-700 text-white"
                      : "text-slate-500 hover:text-slate-300"
                  }`}
                >
                  <Pencil className="h-3 w-3" />
                  Write
                </button>
                <button
                  type="button"
                  aria-pressed={descMode === "preview"}
                  onClick={() => setDescMode("preview")}
                  className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
                    descMode === "preview"
                      ? "bg-slate-700 text-white"
                      : "text-slate-500 hover:text-slate-300"
                  }`}
                >
                  <Eye className="h-3 w-3" />
                  Preview
                </button>
              </div>
              {descMode === "write" ? (
                <textarea
                  aria-label="Description"
                  value={description}
                  onChange={(e) => {
                    setDescription(e.target.value);
                    markDirty();
                  }}
                  rows={14}
                  placeholder="Add a description (supports markdown)…"
                  className="min-h-[16rem] w-full resize-y rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2 font-mono text-sm text-slate-300 placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              ) : description ? (
                <div
                  data-testid="description-preview"
                  className="prose-card min-h-[16rem] overflow-y-auto rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2"
                >
                  <Markdown>{description}</Markdown>
                </div>
              ) : (
                <div
                  onClick={() => setDescMode("write")}
                  className="min-h-[16rem] cursor-text rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2 text-sm text-slate-600"
                >
                  Add a description…
                </div>
              )}
            </div>

            <div>
              <h3 className={SECTION_HEADING}>Subtasks</h3>
              <ul className="space-y-1.5">
                {subtasks.map((sub) => (
                  <li
                    key={sub.id}
                    className="group flex items-center gap-2.5 rounded-md px-2 py-1.5 hover:bg-slate-800/50"
                  >
                    <button
                      type="button"
                      aria-label={`${sub.completed ? "Mark not done" : "Mark done"}: ${sub.title}`}
                      aria-pressed={sub.completed}
                      onClick={() => handleToggleSubtask(sub.id)}
                      className="shrink-0"
                    >
                      {sub.completed ? (
                        <CheckCircle2 className="h-4 w-4 text-green-500" />
                      ) : (
                        <Circle className="h-4 w-4 text-slate-600" />
                      )}
                    </button>
                    <span
                      className={`flex-1 text-sm ${
                        sub.completed ? "text-slate-600 line-through" : "text-slate-300"
                      }`}
                    >
                      {sub.title}
                    </span>
                    <button
                      type="button"
                      aria-label={`Delete subtask: ${sub.title}`}
                      onClick={() => handleDeleteSubtask(sub.id)}
                      className="text-slate-600 opacity-0 transition-all hover:text-red-400 focus:opacity-100 group-hover:opacity-100"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </li>
                ))}
              </ul>
              <form onSubmit={handleAddSubtask} className="mt-2 flex gap-2">
                <input
                  aria-label="New subtask"
                  value={newSubtask}
                  onChange={(e) => setNewSubtask(e.target.value)}
                  placeholder="Add subtask…"
                  className="flex-1 rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
                <button
                  type="submit"
                  className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-300 transition-colors hover:bg-slate-700"
                >
                  Add
                </button>
              </form>
            </div>
            {/* Agent request history goes here, last in this column. */}
          </section>

          <aside
            aria-label="Ticket fields"
            className="space-y-6 border-t border-slate-800 p-4 sm:p-6 lg:overflow-y-auto lg:border-l lg:border-t-0"
          >
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label htmlFor={statusId} className={FIELD_LABEL}>
                  Status
                </label>
                <select
                  id={statusId}
                  value={status}
                  onChange={(e) => {
                    setStatus(e.target.value);
                    markDirty();
                  }}
                  className={SELECT}
                >
                  {STATUSES.map((s) => (
                    <option key={s} value={s}>
                      {STATUS_LABELS[s]}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={priorityId} className={FIELD_LABEL}>
                  Priority
                </label>
                <select
                  id={priorityId}
                  value={priority}
                  onChange={(e) => {
                    setPriority(e.target.value);
                    markDirty();
                  }}
                  className={`${SELECT} capitalize`}
                >
                  {PRIORITIES.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={dueDateId} className={FIELD_LABEL}>
                  Due Date
                </label>
                <input
                  id={dueDateId}
                  type="date"
                  value={dueDate}
                  onChange={(e) => {
                    setDueDate(e.target.value);
                    markDirty();
                  }}
                  className={SELECT}
                />
              </div>
              <div>
                <span className={FIELD_LABEL}>Project</span>
                <div className="truncate px-3 py-2 text-sm text-slate-400">
                  {projects.find((p) => p.id === ticket.projectId)?.name || "—"}
                </div>
              </div>
            </div>

            <div>
              <h3 className={SECTION_HEADING}>Repos</h3>
              <RepoPicker
                value={repos}
                onChange={(next) => {
                  setRepos(next);
                  markDirty();
                }}
              />
            </div>

            <div>
              <h3 className={SECTION_HEADING}>Labels</h3>
              <LabelPicker
                value={labels}
                onChange={(next) => {
                  setLabels(next);
                  markDirty();
                }}
              />
            </div>

            <div>
              <h3 className={SECTION_HEADING}>Depends on</h3>
              <DependencyPicker
                value={dependsOn}
                excludeTicketId={ticket.id}
                onChange={(next) => {
                  setDependsOn(next);
                  markDirty();
                }}
              />
            </div>

            {detail.blocks && detail.blocks.length > 0 && (
              <div>
                <h3 className={SECTION_HEADING}>Blocks</h3>
                {detail.blocks.map((ref) => (
                  <div
                    key={ref.id}
                    className="mb-1.5 flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5 opacity-75"
                  >
                    <span className="min-w-[52px] font-mono text-[11px] text-slate-400">{ref.key}</span>
                    <span className="flex-1 truncate text-[12.5px] text-slate-300">{ref.title}</span>
                    <span className="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
                      {ref.status.replace("_", " ")}
                    </span>
                  </div>
                ))}
                <p className="mt-1 text-[11px] text-slate-600">
                  Read only. Derived from other tickets that depend on this one.
                </p>
              </div>
            )}
          </aside>
        </div>

        {confirmOpen && (
          <DiscardConfirm ref={confirmRef} onCancel={cancelDiscard} onDiscard={acceptDiscard} />
        )}
      </div>
    </div>
  );
}

// "Discard unsaved changes?", shown over the editor. Focus starts on the safe
// choice; a click beside the box cancels, and so does Escape (handled by the
// editor, which knows the confirm is open).
function DiscardConfirm({
  ref,
  onCancel,
  onDiscard,
}: {
  ref: React.Ref<HTMLDivElement>;
  onCancel: () => void;
  onDiscard: () => void;
}) {
  const ids = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    cancelRef.current?.focus();
  }, []);

  return (
    <div className="absolute inset-0 z-20 flex items-center justify-center p-4">
      <div
        data-testid="discard-confirm-backdrop"
        aria-hidden="true"
        className="absolute inset-0 bg-slate-950/60"
        onClick={onCancel}
      />
      <div
        ref={ref}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={`${ids}-title`}
        aria-describedby={`${ids}-desc`}
        className="relative w-full max-w-sm rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">
          Discard unsaved changes?
        </h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">
          Your edits to this ticket have not been saved.
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 transition-colors hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            Keep editing
          </button>
          <button
            type="button"
            onClick={onDiscard}
            className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400"
          >
            Discard
          </button>
        </div>
      </div>
    </div>
  );
}

// The ticket's URL with a button that copies it.
function TicketLink({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = () => {
    navigator.clipboard
      ?.writeText(url)
      .then(() => setCopied(true))
      .catch(() => {
        // Clipboard access can be refused; the URL stays selectable.
      });
  };

  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <span data-testid="ticket-url" className="truncate font-mono text-xs text-slate-500">
        {url}
      </span>
      <button
        type="button"
        aria-label={copied ? "Link copied" : "Copy link"}
        title={copied ? "Copied" : "Copy link"}
        onClick={copy}
        className="shrink-0 text-slate-500 transition-colors hover:text-slate-300"
      >
        {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}
