import { useCallback, useEffect, useId, useRef, useState } from "react";
import { X, Trash2, CheckCircle2, Circle, Pencil, Eye, Copy, Check, RefreshCw, AlertTriangle } from "lucide-react";
import {
  api,
  type EpicRef,
  type StatusChange,
  type Ticket,
  type Project,
  type Subtask,
  type TicketWrite,
} from "../api/client";
import ActivityList from "./ActivityList";
import DocumentModal from "./DocumentModal";
import OwnerMarkdown from "./OwnerMarkdown";
import DocumentsSection from "./DocumentsSection";
import ImageUploadStatus from "./ImageUploadStatus";
import LabelPicker from "./LabelPicker";
import RepoPicker from "./RepoPicker";
import DependencyPicker, { TicketRefLabel } from "./DependencyPicker";
import { activityEntries } from "../lib/activity";
import { documentWindowKey } from "../lib/documents";
import { useEscape } from "../lib/escapeStack";
import { useDocParam } from "../hooks/useDocParam";
import { useOwnerDocuments } from "../hooks/useOwnerDocuments";
import { usePasteImages } from "../hooks/usePasteImages";
import { actionErrorMessage, deleteErrorMessage, saveErrorMessage } from "../lib/saveError";
import { STATUSES, STATUS_LABELS, STATUS_STYLES, isStatus } from "../lib/status";
import {
  changedFields,
  editedWrite,
  ticketFields,
  toDateInputValue,
  type TicketFields,
} from "../lib/ticketFields";
import { PRIORITIES } from "../lib/priority";

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
  deleted = false,
  closeRequested = false,
  onCloseCancelled,
  onDirtyChange,
  onClose,
  onUpdate,
  onDelete,
  onOpenTicket,
}: {
  ticket: Ticket;
  projects: Project[];
  // The ticket's shareable URL, from useTicketParam. Without one the header
  // shows no link or copy button.
  ticketUrl?: string;
  // The ticket left the loaded tickets: it was deleted elsewhere. Every
  // field, control and Save turn off, a notice says so, and Close is the
  // only action left — it never asks, whatever is unsaved.
  deleted?: boolean;
  // A close asked for from outside the editor: the browser's Back button, or
  // anything else that drops the `ticket` parameter. It goes through the same
  // discard question as the close button, and cancelling it calls
  // onCloseCancelled, which puts the parameter back. Never set once `deleted`
  // is: a read-only editor has nothing left to ask about.
  closeRequested?: boolean;
  onCloseCancelled?: () => void;
  // Reports unsaved edits, so whoever owns the URL knows the editor cannot
  // just be unmounted.
  onDirtyChange?: (dirty: boolean) => void;
  onClose: () => void;
  // Answering with a promise lets the editor wait for the save and keep the
  // edits when it fails; every page does, since each one refetches after it.
  onUpdate: (id: string, data: TicketWrite) => void | Promise<void>;
  // Answering with a promise lets the editor wait for the delete: it only
  // closes once the delete has gone through, and stays open on the fields it
  // had, showing why, when it hasn't.
  onDelete: (id: string) => void | Promise<void>;
  // Opens another ticket, by id, in this editor's place: what the Depends on
  // and Blocks rows do when clicked. Without it they are plain text.
  onOpenTicket?: (id: string) => void;
}) {
  const [title, setTitle] = useState(ticket.title);
  const [description, setDescription] = useState(ticket.description);
  const [status, setStatus] = useState(ticket.status);
  // The status as last saved or synced, which the Note field compares the
  // Status select against, and the note to save with a change of status.
  const [savedStatus, setSavedStatus] = useState(ticket.status);
  const [note, setNote] = useState("");
  const [priority, setPriority] = useState(ticket.priority);
  const [dueDate, setDueDate] = useState(toDateInputValue(ticket.dueDate));
  const [epic, setEpic] = useState(ticket.epic?.id ?? "");
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
  // Whether `saveError` is currently a failed Save's message: the edits it
  // left dirty are still exactly what Save would resend, so it stays on
  // screen until Save (or Delete) is tried again — a subtask action
  // succeeding or failing beside it must not quietly wipe it. An action's own
  // error carries no such claim on the strip.
  const [saveErrorUnresolved, setSaveErrorUnresolved] = useState(false);
  // A delete in flight. Unlike a save there is nothing to keep dirty: a
  // failed delete just leaves the ticket as it was, with the same error strip
  // saying why, rather than closing the editor.
  const [deleting, setDeleting] = useState(false);
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
    epic,
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
    setSavedStatus(full.status);
    setNote("");
    setPriority(full.priority);
    setDueDate(toDateInputValue(full.dueDate));
    setEpic(full.epic?.id ?? "");
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

  // The ticket's documents, loaded here and refreshed on every live change,
  // and the one the URL has open over the editor.
  const { documents, failed: documentsFailed, reload: reloadDocuments } = useOwnerDocuments({ ticketId: ticket.id });
  const docParam = useDocParam(documents);
  const docOpen = docParam.selected !== null;

  // Images pasted or dropped into the description's Write mode.
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const pasteImages = usePasteImages({
    owner: { ticketId: ticket.id },
    documents,
    value: description,
    onChange: (next) => {
      setDescription(next);
      markDirty();
    },
    textareaRef: descriptionRef,
    onUploaded: reloadDocuments,
    savedText: () => baseRef.current.description,
  });

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
  // While the open document holds unsaved text, the document asks first,
  // whichever Back dropped what: the editor's own question waits until the
  // document's is answered. `closeRequested` is never set once the ticket is
  // deleted, but the extra guard here costs nothing and says so plainly.
  const urlAsking = !deleted && closeRequested && dirty && !docParam.dirty;
  const confirmOpen = asking || urlAsking;
  const pendingActionRef = useRef<(() => void) | null>(null);
  const focusBeforeConfirmRef = useRef<HTMLElement | null>(null);
  const ids = useId();
  const keyId = `${ids}-key`;
  const statusId = `${ids}-status`;
  const noteId = `${ids}-note`;
  const noteHelpId = `${ids}-note-help`;
  const priorityId = `${ids}-priority`;
  const dueDateId = `${ids}-due`;
  const epicId = `${ids}-epic`;

  // The project's epics, which the Epic select offers. Reloaded whenever the
  // ticket changes, so one added since the editor opened shows up too. Until
  // they arrive, and if they never do, the select still shows the ticket's
  // own epic.
  const [epics, setEpics] = useState<EpicRef[]>([]);
  useEffect(() => {
    let cancelled = false;
    api.epics
      .list(ticket.projectId)
      .then((list) => {
        if (!cancelled) setEpics((list?.epics ?? []).map(({ id, name }) => ({ id, name })));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [ticket.projectId, ticket.updatedAt]);

  // The ticket's status history for the Activity section, fetched on open and
  // again whenever the ticket changes: a live refresh, or this editor's own
  // save, hands the editor a ticket with a newer `updatedAt`. Until it arrives,
  // and if it never does, the section simply shows nothing yet.
  const [history, setHistory] = useState<StatusChange[] | null>(null);
  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => api.tickets.history(ticket.id))
      .then((changes) => {
        if (!cancelled && Array.isArray(changes)) setHistory(changes);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [ticket.id, ticket.updatedAt]);


  // A document just created with New opens in edit mode. Remembered by id
  // until that document has opened and closed again, so opening it later
  // shows it as usual.
  const [editOnOpen, setEditOnOpen] = useState<{ id: string; opened: boolean } | null>(null);
  const openDocId = docParam.selected?.id ?? null;
  if (editOnOpen) {
    if (!editOnOpen.opened && openDocId === editOnOpen.id) setEditOnOpen({ ...editOnOpen, opened: true });
    else if (editOnOpen.opened && openDocId !== editOnOpen.id) setEditOnOpen(null);
  }

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
  // A deleted ticket never asks: nothing can be saved, so there is nothing to
  // confirm losing.
  const confirmDiscardThen = useCallback(
    (action: () => void) => {
      if (confirmOpen) return;
      if (deleted || !dirty) {
        action();
        return;
      }
      const active = document.activeElement;
      focusBeforeConfirmRef.current = active instanceof HTMLElement ? active : null;
      pendingActionRef.current = action;
      setAsking(true);
    },
    [deleted, dirty, confirmOpen],
  );

  const requestClose = useCallback(() => confirmDiscardThen(onClose), [confirmDiscardThen, onClose]);

  // Following a link to another ticket leaves this one, so it asks about
  // unsaved edits the way closing does.
  const openLinked = onOpenTicket
    ? (id: string) => confirmDiscardThen(() => onOpenTicket(id))
    : undefined;

  // The notice's reload: the latest version replaces the edits, so it asks
  // first, down the same path every other discard takes.
  const reloadChanged = () =>
    confirmDiscardThen(() => {
      const latest = changedRef.current;
      if (latest) syncFrom(latest);
    });

  // Tell whoever owns the URL about unsaved edits, so a Back that drops the
  // `ticket` parameter keeps the editor mounted long enough to ask about them
  // rather than unmounting it. A document's unsaved text counts too: the
  // editor unmounting would take the document with it.
  const holding = dirty || docParam.dirty;
  useEffect(() => {
    onDirtyChange?.(holding);
  }, [holding, onDirtyChange]);

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
    if (closeRequested && !holding) onCloseRef.current();
  }, [closeRequested, holding]);

  // Review 1: a discard question already on screen (Close, Escape or the
  // scrim asked while the ticket still existed) has nothing left to ask
  // about once it is deleted: dismiss it, leaving only the read-only
  // notice, and give focus back to the dialog the way cancelling it does.
  useEffect(() => {
    if (!deleted) return;
    pendingActionRef.current = null;
    setAsking(false);
    const dialog = dialogRef.current;
    setTimeout(() => dialog?.focus());
  }, [deleted]);

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

  // Escape asks to close the editor. A layer over it (the discard confirm, a
  // document, a rename field) takes Escape first through lib/escapeStack,
  // which marks the event handled, and so does any control that handles
  // Escape itself; this listener leaves a handled event alone.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || e.isComposing || confirmOpen) return;
      e.preventDefault();
      requestClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [confirmOpen, requestClose]);

  // Keep Tab and Shift+Tab inside the dialog, or inside the confirm while it
  // is open. A deleted ticket's fields, subtasks and documents sit under an
  // `inert` ancestor (see `deleted` below): querySelectorAll still finds
  // them, since `inert` is only a browser interaction rule, so they are
  // filtered out here or Tab would land on a control that does nothing.
  const handleTrapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = confirmOpen ? confirmRef.current : dialogRef.current;
    if (e.key !== "Tab" || !root) return;
    const focusable = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => !el.closest("[inert]"),
    );
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

  // Counts edits, so a save can tell whether the fields changed while it
  // was on its way (typing, or a failed image's reference taken out).
  const editsRef = useRef(0);
  const markDirty = () => {
    editsRef.current++;
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
  //
  // A note goes with the save only when the save changes the status; it never
  // holds the save up.
  const handleSave = async () => {
    // The Save button is hidden once the ticket is deleted; this guards the
    // same case defensively.
    if (deleted) return;
    const current = currentFields();
    const editsAtSave = editsRef.current;
    const write = editedWrite(baseRef.current, current);
    if (write.status !== undefined && note.trim()) write.note = note.trim();
    setSaveError(null);
    setSaveErrorUnresolved(false);
    setSaving(true);
    try {
      await onUpdate(ticket.id, write);
    } catch (error) {
      setSaveError(saveErrorMessage(error));
      setSaveErrorUnresolved(true);
      return;
    } finally {
      setSaving(false);
    }
    baseRef.current = current;
    setSavedStatus(current.status);
    setNote("");
    noteChanged(null);
    // An edit made while the save was on its way is not saved yet.
    if (editsRef.current !== editsAtSave) return;
    dirtyRef.current = false;
    setDirty(false);
  };

  // Delete asks nothing (there is no confirm for it) and closes the editor
  // once the ticket is actually gone. A failed delete — the ticket already
  // gone, the server refusing it, the network down — leaves the editor open
  // on the fields it had, with the same error strip a failed save uses.
  const handleDelete = async () => {
    if (deleted || deleting) return;
    setSaveError(null);
    setSaveErrorUnresolved(false);
    setDeleting(true);
    try {
      await onDelete(ticket.id);
    } catch (error) {
      setSaveError(deleteErrorMessage(error));
      return;
    } finally {
      setDeleting(false);
    }
    onClose();
  };

  // These three take effect the moment they're clicked, with no Save of their
  // own to fail instead: a rejected call is reported right here, on the same
  // error strip a failed save or delete uses, rather than left to become an
  // unhandled rejection. Each clears a stale error of its own kind before it
  // starts, so it never lingers past a later action that succeeded — but
  // never an unresolved Save failure: those edits are still sitting there
  // unsent, and a subtask succeeding or failing beside them says nothing
  // about whether they went through.
  const clearActionError = () => {
    if (!saveErrorUnresolved) setSaveError(null);
  };
  const reportActionError = (error: unknown) => {
    if (!saveErrorUnresolved) setSaveError(actionErrorMessage(error));
  };
  const handleAddSubtask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newSubtask.trim()) return;
    clearActionError();
    try {
      const sub = await api.tickets.addSubtask(ticket.id, newSubtask);
      setSubtasks((prev) => [...prev, sub]);
      setNewSubtask("");
    } catch (error) {
      reportActionError(error);
    }
  };

  const handleToggleSubtask = async (id: string) => {
    clearActionError();
    try {
      const updated = await api.subtasks.toggle(id);
      setSubtasks((prev) => prev.map((s) => (s.id === id ? updated : s)));
    } catch (error) {
      reportActionError(error);
    }
  };

  const handleDeleteSubtask = async (id: string) => {
    clearActionError();
    try {
      await api.subtasks.delete(id);
      setSubtasks((prev) => prev.filter((s) => s.id !== id));
    } catch (error) {
      reportActionError(error);
    }
  };

  const ticketKey = `${ticket.projectPrefix}-${ticket.number}`;
  // "No epic", then the project's epics by name, plus the ticket's own epic if
  // the list doesn't have it (yet), so the select never shows a blank.
  const epicOptions = [...epics];
  for (const own of [detail.epic, ticket.epic]) {
    if (own && !epicOptions.some((e) => e.id === own.id)) epicOptions.push(own);
  }
  epicOptions.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
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
        inert={docOpen}
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
          {dirty && !deleted && (
            <button
              type="button"
              onClick={handleSave}
              disabled={saving}
              className="shrink-0 whitespace-nowrap rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-60"
            >
              Save Changes
            </button>
          )}
          {!deleted && (
            <button
              type="button"
              aria-label="Delete ticket"
              title="Delete ticket"
              onClick={handleDelete}
              disabled={deleting}
              className="shrink-0 text-slate-500 transition-colors hover:text-red-400 disabled:opacity-60"
            >
              <Trash2 className="h-4 w-4" />
            </button>
          )}
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

        {deleted && (
          <div
            role="status"
            data-testid="ticket-deleted-notice"
            className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-200 sm:px-6"
          >
            <span>This ticket was deleted. It is shown read-only.</span>
          </div>
        )}

        {!deleted && saveError && (
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

        {!deleted && changed && (
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
              readOnly={deleted}
              onChange={(e) => {
                if (deleted) return;
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
                  ref={descriptionRef}
                  aria-label="Description"
                  value={description}
                  readOnly={deleted}
                  {...(deleted ? {} : pasteImages.textareaProps)}
                  onChange={(e) => {
                    if (deleted) return;
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
                  <OwnerMarkdown documents={documents ?? (documentsFailed ? [] : null)}>{description}</OwnerMarkdown>
                </div>
              ) : (
                <div
                  onClick={() => setDescMode("write")}
                  className="min-h-[16rem] cursor-text rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2 text-sm text-slate-600"
                >
                  Add a description…
                </div>
              )}
              <ImageUploadStatus
                uploads={pasteImages.uploads}
                problems={pasteImages.problems}
                added={pasteImages.added}
                onDismiss={pasteImages.dismissProblems}
              />
            </div>

            <div inert={deleted} className={deleted ? "opacity-60" : ""}>
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

            <div inert={deleted} className={deleted ? "opacity-60" : ""}>
              <DocumentsSection
                documents={documents}
                failed={documentsFailed}
                notice={docParam.notice}
                onDismissNotice={docParam.dismissNotice}
                onOpen={docParam.open}
                onChanged={reloadDocuments}
                owner={{ ticketId: ticket.id }}
                onCreated={(doc, edit) => {
                  // New opens it, in edit mode, once the list carries it: this
                  // reload or a live refresh, whichever lands first with it.
                  if (edit) {
                    setEditOnOpen({ id: doc.id, opened: false });
                    docParam.openWhenListed(doc);
                  }
                  reloadDocuments();
                }}
              />
            </div>

            {/* Last in this column. Agent requests and comments join this list as more kinds of entry. */}
            <div>
              <h3 className={SECTION_HEADING}>Activity</h3>
              {history && <ActivityList entries={activityEntries(history)} />}
            </div>
          </section>

          <aside
            aria-label="Ticket fields"
            inert={deleted}
            className={`space-y-6 border-t border-slate-800 p-4 sm:p-6 lg:overflow-y-auto lg:border-l lg:border-t-0 ${
              deleted ? "opacity-60" : ""
            }`}
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
                    // Back to the saved status there is no change to note.
                    if (e.target.value === savedStatus) setNote("");
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
                <label htmlFor={epicId} className={FIELD_LABEL}>
                  Epic
                </label>
                <select
                  id={epicId}
                  value={epic}
                  onChange={(e) => {
                    setEpic(e.target.value);
                    markDirty();
                  }}
                  className={SELECT}
                >
                  <option value="">No epic</option>
                  {epicOptions.map((e) => (
                    <option key={e.id} value={e.id}>
                      {e.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <span className={FIELD_LABEL}>Project</span>
                <div className="truncate px-3 py-2 text-sm text-slate-400">
                  {projects.find((p) => p.id === ticket.projectId)?.name || "—"}
                </div>
              </div>
            </div>

            {status !== savedStatus && (
              <div>
                <label htmlFor={noteId} className={FIELD_LABEL}>
                  Note (optional)
                </label>
                <textarea
                  id={noteId}
                  value={note}
                  onChange={(e) => {
                    setNote(e.target.value);
                    markDirty();
                  }}
                  rows={3}
                  placeholder="Why the status changed"
                  aria-describedby={noteHelpId}
                  className="w-full resize-y rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
                <p id={noteHelpId} className="mt-1 text-[11px] text-slate-600">
                  Saved with the change and shown in Activity.
                </p>
              </div>
            )}

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
                onOpen={openLinked}
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
                    className="mb-1.5 flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5 opacity-75 hover:opacity-100"
                  >
                    <TicketRefLabel ticketRef={ref} onOpen={openLinked} />
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

      {/* A sibling of the dialog, not inside it, so the editor's Tab trap never
          sees the document's keys; the editor is inert while it is open. */}
      {docParam.selected && (
        <DocumentModal
          key={documentWindowKey(docParam.selected)}
          doc={docParam.selected}
          documents={documents ?? []}
          owner={{ ticketId: ticket.id }}
          ownerLabel={ticketKey}
          startEditing={editOnOpen?.id === docParam.selected.id}
          deleted={docParam.deleted}
          ticketDeleted={deleted}
          closeRequested={docParam.closeRequested || (docParam.dirty && !!closeRequested)}
          onCloseCancelled={() => {
            // Keep editing undoes every Back the question stood for: the
            // ticket's parameter first (when a Back dropped it too), then the
            // document's on top, so each is its own entry again.
            if (closeRequested) onCloseCancelled?.();
            docParam.cancelClose();
          }}
          onDirtyChange={docParam.onDirtyChange}
          onStep={docParam.step}
          onImageAdded={reloadDocuments}
          onClose={docParam.close}
          onRenamed={(doc) => {
            docParam.renamed(doc);
            reloadDocuments();
          }}
          onDeleted={() => {
            docParam.close();
            reloadDocuments();
          }}
          onRecreated={(doc) => {
            // The copy takes the old name: once the list carries it, the URL
            // names it and the modal shows it.
            docParam.recreated(doc);
            reloadDocuments();
          }}
        />
      )}
    </div>
  );
}

// "Discard unsaved changes?", shown over the editor. Focus starts on the safe
// choice; a click beside the box cancels, and so does Escape: the confirm is
// the topmost Escape layer while it is open, above a rename field or anything
// else the editor has open under it.
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
  useEscape(onCancel);

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
