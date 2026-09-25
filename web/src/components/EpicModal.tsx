import { useEffect, useId, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import { api, type Epic } from "../api/client";
import { useDocParam } from "../hooks/useDocParam";
import { useOwnerDocuments } from "../hooks/useOwnerDocuments";
import { useEscape } from "../lib/escapeStack";
import DocumentModal from "./DocumentModal";
import DocumentsSection from "./DocumentsSection";
import EpicForm from "./EpicForm";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// An epic: its name and description, saved with Save, which closes the
// modal as the Edit epic dialog did (and, as there, typing in those two
// fields is dropped without asking), and its documents, which act at once,
// as they do in the ticket editor. A document opens over it, its name in the
// URL beside the epic's.
//
// A document's unsaved text is never dropped without asking, whatever asked
// for it: the modal reports it as its own (onDirtyChange), so a Back that
// drops the epic keeps the modal mounted (closeRequested) while the document
// asks, and Keep editing puts back every entry the Backs took.
export default function EpicModal({
  epic,
  epics,
  projectPrefix,
  closeRequested = false,
  onCloseCancelled,
  onDirtyChange,
  onClose,
  onSaved,
}: {
  epic: Epic;
  /** The project's epics, which the name must differ from. */
  epics: readonly Epic[];
  projectPrefix: string;
  /** Back dropped the epic while its document holds unsaved text. */
  closeRequested?: boolean;
  /** Keep editing after such a Back: put the epic back in the URL. */
  onCloseCancelled?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onClose: () => void;
  onSaved: (epic: Epic) => void | Promise<void>;
}) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const owner = useMemo(() => ({ epicId: epic.id }), [epic.id]);
  const { documents, failed, reload } = useOwnerDocuments(owner);
  const docParam = useDocParam(documents, "epic");
  useEscape(onClose);

  // Focus moves into the modal on open (the name field takes it, being
  // autofocused), and the page gives it back to the opener once closed.
  useEffect(() => {
    if (!dialogRef.current?.contains(document.activeElement)) dialogRef.current?.focus();
  }, []);

  // A document just created with New opens in edit mode. Remembered by id
  // until that document has opened and closed again, so opening it later
  // shows it as usual.
  const [editOnOpen, setEditOnOpen] = useState<{ id: string; opened: boolean } | null>(null);
  const openDocId = docParam.selected?.id ?? null;
  if (editOnOpen) {
    if (!editOnOpen.opened && openDocId === editOnOpen.id) setEditOnOpen({ ...editOnOpen, opened: true });
    else if (editOnOpen.opened && openDocId !== editOnOpen.id) setEditOnOpen(null);
  }

  // The document's unsaved text is the modal's: unmounting the modal would
  // take the document with it.
  useEffect(() => {
    onDirtyChange?.(docParam.dirty);
  }, [docParam.dirty, onDirtyChange]);

  // Keep Tab and Shift+Tab inside the modal.
  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = dialogRef.current;
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
    <div className="fixed inset-0 z-50 flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={trapTab}
        inert={docParam.selected !== null}
        className="relative mx-auto my-auto flex max-h-full w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 px-5 py-3">
          <h2 id={titleId} className="flex-1 text-base font-semibold text-white">
            Edit epic
          </h2>
          <span className="font-mono text-xs text-slate-500">{projectPrefix}</span>
          <button
            type="button"
            aria-label="Close"
            title="Close"
            onClick={onClose}
            className="text-slate-500 transition-colors hover:text-slate-300"
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-5">
          <EpicForm
            epic={epic}
            epics={epics}
            onCancel={onClose}
            onSave={async (data) => {
              await onSaved(await api.epics.update(epic.id, data));
            }}
          />
          <DocumentsSection
            owner={owner}
            ownerNoun="epic"
            documents={documents}
            failed={failed}
            notice={docParam.notice}
            onDismissNotice={docParam.dismissNotice}
            onOpen={docParam.open}
            onChanged={reload}
            onCreated={(doc, edit) => {
              // New opens it, in edit mode, once the list carries it: this
              // reload or a live refresh, whichever lands first with it.
              if (edit) {
                setEditOnOpen({ id: doc.id, opened: false });
                docParam.openWhenListed(doc);
              }
              reload();
            }}
          />
        </div>
      </div>

      {docParam.selected && (
        <DocumentModal
          key={docParam.selected.id}
          doc={docParam.selected}
          documents={documents ?? []}
          owner={owner}
          ownerLabel={epic.name}
          ownerNoun="epic"
          startEditing={editOnOpen?.id === docParam.selected.id}
          deleted={docParam.deleted}
          closeRequested={docParam.closeRequested || (docParam.dirty && closeRequested)}
          onCloseCancelled={() => {
            // Keep editing undoes every Back the question stood for: the
            // epic's parameter first (when a Back dropped it too), then the
            // document's on top, so each is its own entry again.
            if (closeRequested) onCloseCancelled?.();
            docParam.cancelClose();
          }}
          onDirtyChange={docParam.onDirtyChange}
          onClose={docParam.close}
          onRenamed={(doc) => {
            docParam.renamed(doc);
            reload();
          }}
          onDeleted={() => {
            docParam.close();
            reload();
          }}
          onRecreated={(doc) => {
            // The copy takes the old name: once the list carries it, the URL
            // names it and the modal shows it.
            docParam.recreated(doc);
            reload();
          }}
        />
      )}
    </div>
  );
}
