import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Download, Eye, Pencil, Trash2, X } from "lucide-react";
import Markdown from "react-markdown";
import { api, type DocumentMeta, type DocumentOwnerRef, type DocumentWithContent } from "../api/client";
import { activityTime } from "../lib/activity";
import { conflictDocument, contentTooLarge, displayName, formatSize, isNotFound } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";
const TOGGLE = "inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors";
const NOTICE =
  "flex flex-wrap items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-200 sm:px-6";
const NOTICE_BUTTON =
  "rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 hover:bg-amber-500/20 focus:outline-none focus:ring-1 focus:ring-amber-300";

type Saved = { revision: number; content: string };

// A document over the ticket editor, as large as the editor (same padding
// and max width). The content is fetched on open and again when its
// revision changes, which is how an agent's save reaches an open document.
// The header renames, downloads and deletes; Edit (markdown only) switches
// to the description's Write/Preview toggle with Save and Cancel.
//
// A save sends the revision the text started from, so one made after
// someone else's is refused (409) rather than overwriting it. While editing,
// a newer revision arriving live raises the conflict notice if there is
// unsaved text, and quietly replaces the text if there is none. Unsaved text
// is never dropped without asking "Discard your changes?", whatever asked
// for it: Cancel, Escape, ×, the scrim, or Back (closeRequested, from
// useDocParam). A document deleted mid-edit stays on screen (deleted, also
// from useDocParam) and offers to save the text as a new document.
export default function DocumentModal({
  doc,
  documents,
  owner,
  ownerLabel,
  ownerNoun = "ticket",
  startEditing = false,
  deleted = false,
  closeRequested = false,
  onClose,
  onCloseCancelled,
  onDirtyChange,
  onRenamed,
  onDeleted,
  onRecreated,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  owner: DocumentOwnerRef;
  ownerLabel: string;
  ownerNoun?: string;
  /** Open straight into edit mode (a document just created with New). */
  startEditing?: boolean;
  /** The document was deleted while this held unsaved edits. */
  deleted?: boolean;
  /** Back dropped the document while this held unsaved edits: ask. */
  closeRequested?: boolean;
  onClose: () => void;
  /** The user kept editing after a Back: put the document back. */
  onCloseCancelled?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onRenamed: (doc: DocumentMeta) => void;
  onDeleted: () => void;
  /** "Save as a new document" created this copy of a deleted one. */
  onRecreated: (doc: DocumentMeta) => void;
}) {
  const editable = doc.format === "markdown";
  const [saved, setSaved] = useState<Saved | null>(null);
  const [failed, setFailed] = useState(false);
  const [editing, setEditing] = useState(startEditing && editable);
  // What the draft started from: its revision is what a save says it is
  // overwriting, its content what "unsaved" compares against.
  const [base, setBase] = useState<Saved | null>(null);
  const [draft, setDraft] = useState("");
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [conflict, setConflict] = useState<DocumentWithContent | null>(null);
  const [goneOnSave, setGoneOnSave] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // The action waiting on "Discard your changes?", if one asked.
  const [pending, setPending] = useState<(() => void) | null>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const shown = displayName(doc);

  const dirty = editing && base !== null && draft !== base.content;
  const urlAsking = closeRequested && dirty;
  const asking = pending !== null || urlAsking;
  const gone = deleted || goneOnSave;

  // The fetch reads these without re-running on each keystroke.
  const editingRef = useRef(editing);
  const baseRef = useRef(base);
  const dirtyRef = useRef(dirty);
  const savingRef = useRef(saving);
  useEffect(() => {
    editingRef.current = editing;
    baseRef.current = base;
    dirtyRef.current = dirty;
    savingRef.current = saving;
  });

  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => api.documents.get(doc.id))
      .then((full) => {
        if (cancelled) return;
        const next = { revision: full.revision, content: full.content };
        setSaved(next);
        setFailed(false);
        if (!editingRef.current) return;
        const from = baseRef.current;
        if (from === null || (!dirtyRef.current && full.revision !== from.revision)) {
          // Nothing typed yet: start from, or move to, the newest text.
          setBase(next);
          setDraft(full.content);
        } else if (full.revision !== from.revision && !savingRef.current) {
          // A save in flight answers for itself: our own save's echo is not
          // a conflict, and someone else's comes back as a 409.
          setConflict(full);
        }
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.revision]);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  const stopEditing = useCallback(() => {
    setEditing(false);
    setBase(null);
    setConflict(null);
    setGoneOnSave(false);
    setSaveError(null);
  }, []);

  // Runs `action` now, or once the user agrees to lose unsaved text.
  const confirmDiscardThen = useCallback(
    (action: () => void) => {
      if (asking) return;
      if (!dirty) action();
      else setPending(() => action);
    },
    [asking, dirty],
  );

  const requestClose = useCallback(() => confirmDiscardThen(onClose), [confirmDiscardThen, onClose]);
  useEscape(requestClose);

  const acceptDiscard = () => {
    const action = pending ?? (urlAsking ? onClose : null);
    setPending(null);
    stopEditing();
    action?.();
  };
  const cancelDiscard = () => {
    const undoBack = pending === null && urlAsking;
    setPending(null);
    if (undoBack) onCloseCancelled?.();
  };

  const startEdit = () => {
    if (!saved) return;
    setEditing(true);
    setMode("write");
    setBase(saved);
    setDraft(saved.content);
    setSaveError(null);
  };

  const save = async () => {
    if (!base || saving) return;
    const tooLarge = contentTooLarge(draft);
    if (tooLarge) {
      setSaveError(tooLarge);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.documents.update(doc.id, { content: draft, expectedRevision: base.revision });
      setSaved({ revision: updated.revision, content: updated.content });
      stopEditing();
    } catch (err) {
      const current = conflictDocument(err);
      if (current) setConflict(current);
      else if (isNotFound(err)) setGoneOnSave(true);
      else setSaveError(serverMessage(err, "The document was not saved."));
    } finally {
      setSaving(false);
    }
  };

  // Keep my text; the next save overwrites the revision I have now seen.
  const keepMine = () => {
    if (conflict && base) setBase({ revision: conflict.revision, content: base.content });
    setConflict(null);
  };
  const loadTheirs = () => {
    if (conflict) setSaved({ revision: conflict.revision, content: conflict.content });
    stopEditing();
  };
  const saveAsNew = async () => {
    if (saving) return;
    const tooLarge = contentTooLarge(draft);
    if (tooLarge) {
      setSaveError(tooLarge);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const created = await api.documents.create({ ...owner, name: doc.name, format: doc.format, content: draft });
      onRecreated(created);
    } catch (err) {
      setSaveError(serverMessage(err, "The document was not saved."));
    } finally {
      setSaving(false);
    }
  };
  const discardGone = () => {
    stopEditing();
    onClose();
  };

  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = dialogRef.current;
    if (e.key !== "Tab" || !root || deleting || asking) return;
    const focusable = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === root)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  const writing = editing && base !== null && mode === "write";
  let body: React.ReactNode;
  if (writing) {
    body = (
      <textarea
        aria-label="Document content"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        autoFocus
        placeholder="Write markdown…"
        className="min-h-[20rem] w-full flex-1 resize-none rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2 font-mono text-sm text-slate-200 placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
      />
    );
  } else if (editing && base !== null) {
    body = draft ? (
      <div data-testid="document-preview" className="prose-card">
        <Markdown>{draft}</Markdown>
      </div>
    ) : (
      <p className="text-sm text-slate-600">Nothing to preview.</p>
    );
  } else if (failed) body = <p className="text-sm text-slate-500">This document could not be loaded.</p>;
  else if (saved === null) body = <p className="text-sm text-slate-600">Loading…</p>;
  else if (saved.content === "") body = <p className="text-sm text-slate-600">This document is empty.</p>;
  else
    body = (
      <div data-testid="document-content" className="prose-card">
        <Markdown>{saved.content}</Markdown>
      </div>
    );

  return (
    <div className="fixed inset-0 z-[60] flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={requestClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={renaming ? undefined : titleId}
        aria-label={renaming ? shown : undefined}
        tabIndex={-1}
        onKeyDown={trapTab}
        inert={asking}
        className="relative mx-auto flex h-full w-full max-w-[96rem] flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 px-4 py-3 sm:px-6">
          <span className="shrink-0 font-mono text-xs text-slate-400">{ownerLabel}</span>
          <span aria-hidden="true" className="text-slate-600">›</span>
          {renaming ? (
            <DocumentRenameField
              doc={doc}
              documents={documents}
              ownerNoun={ownerNoun}
              onCancel={() => setRenaming(false)}
              onRenamed={(updated) => {
                setRenaming(false);
                onRenamed(updated);
              }}
            />
          ) : (
            <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold text-white">
              {shown}
            </h2>
          )}
          {editing ? (
            <>
              {dirty && (
                <span className="shrink-0 rounded-full bg-amber-500/15 px-2 py-0.5 text-[11px] font-medium text-amber-300">
                  unsaved
                </span>
              )}
              <div className="flex shrink-0 items-center gap-1">
                <button
                  type="button"
                  aria-pressed={mode === "write"}
                  onClick={() => setMode("write")}
                  className={`${TOGGLE} ${mode === "write" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}
                >
                  <Pencil className="h-3 w-3" />
                  Write
                </button>
                <button
                  type="button"
                  aria-pressed={mode === "preview"}
                  onClick={() => setMode("preview")}
                  className={`${TOGGLE} ${mode === "preview" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}
                >
                  <Eye className="h-3 w-3" />
                  Preview
                </button>
              </div>
              <button
                type="button"
                onClick={() => confirmDiscardThen(stopEditing)}
                className="shrink-0 px-2 py-1 text-sm text-slate-400 hover:text-white"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={save}
                disabled={saving || gone || base === null}
                className="shrink-0 rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-60"
              >
                Save
              </button>
            </>
          ) : (
            <>
              <span className="hidden shrink-0 whitespace-nowrap text-xs text-slate-500 sm:inline">
                {formatSize(doc.size)} · updated {activityTime(doc.updatedAt)}
              </span>
              {editable && (
                <button
                  type="button"
                  onClick={startEdit}
                  disabled={saved === null}
                  className="inline-flex shrink-0 items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 transition-colors hover:bg-slate-800 disabled:opacity-60"
                >
                  Edit
                </button>
              )}
              <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
                <Download className="h-4 w-4" />
              </a>
              <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(true)} className={ICON_BUTTON}>
                <Pencil className="h-4 w-4" />
              </button>
              <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(true)} className={`${ICON_BUTTON} hover:text-red-400`}>
                <Trash2 className="h-4 w-4" />
              </button>
            </>
          )}
          <button type="button" aria-label="Close document" title="Close" onClick={requestClose} className={ICON_BUTTON}>
            <X className="h-5 w-5" />
          </button>
        </header>

        {editing && gone && (
          <div role="status" className={NOTICE}>
            <span className="flex-1">This document was deleted while you were editing. Your text isn't saved yet.</span>
            <button type="button" onClick={saveAsNew} disabled={saving} className={`${NOTICE_BUTTON} disabled:opacity-60`}>
              Save as a new document
            </button>
            <button type="button" onClick={discardGone} className={NOTICE_BUTTON}>
              Discard
            </button>
          </div>
        )}
        {editing && !gone && conflict && (
          <div role="status" className={NOTICE}>
            <span className="flex-1">This document changed while you were editing. Your text isn't saved yet.</span>
            <button type="button" onClick={keepMine} className={NOTICE_BUTTON}>
              Keep editing, overwrite on save
            </button>
            <button type="button" onClick={loadTheirs} className={NOTICE_BUTTON}>
              Discard mine, load theirs
            </button>
          </div>
        )}
        {saveError && (
          <p role="alert" className="border-b border-red-500/40 bg-red-500/10 px-4 py-2 text-xs text-red-200 sm:px-6">
            {saveError}
          </p>
        )}

        <div className={`min-h-0 flex-1 px-4 py-4 sm:px-8 sm:py-6 ${writing ? "flex flex-col" : "overflow-y-auto"}`}>
          {body}
        </div>
      </div>

      {deleting && (
        <DeleteDocumentConfirm
          doc={doc}
          onCancel={() => setDeleting(false)}
          onDeleted={() => {
            setDeleting(false);
            onDeleted();
          }}
        />
      )}
      {asking && <DiscardDocumentConfirm onCancel={cancelDiscard} onDiscard={acceptDiscard} />}
    </div>
  );
}

// "Discard your changes?" over the document. Focus starts on the safe
// choice; a click beside the box cancels, and so does Escape: the question
// is the topmost layer on the escape stack while it is open, so Escape
// never reaches the document under it.
function DiscardDocumentConfirm({ onCancel, onDiscard }: { onCancel: () => void; onDiscard: () => void }) {
  const ids = useId();
  const keepRef = useRef<HTMLButtonElement>(null);
  useEscape(onCancel);
  useEffect(() => keepRef.current?.focus(), []);
  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center p-4">
      <div aria-hidden="true" className="absolute inset-0 bg-slate-950/60" onClick={onCancel} />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={`${ids}-title`}
        aria-describedby={`${ids}-desc`}
        className="relative w-full max-w-sm rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">
          Discard your changes?
        </h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">
          Your edits to this document have not been saved.
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={keepRef}
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            Keep editing
          </button>
          <button
            type="button"
            onClick={onDiscard}
            className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400"
          >
            Discard
          </button>
        </div>
      </div>
    </div>
  );
}
