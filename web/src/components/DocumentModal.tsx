import { useEffect, useId, useRef, useState } from "react";
import { Download, Pencil, Trash2, X } from "lucide-react";
import Markdown from "react-markdown";
import { api, type DocumentMeta } from "../api/client";
import { activityTime } from "../lib/activity";
import { displayName, formatSize } from "../lib/documents";
import { useEscape } from "../lib/escapeStack";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";

// A document over the ticket editor, as large as the editor (same padding
// and max width). The content is
// fetched on open and again when its revision changes, which is how an
// agent's save reaches an open document. Escape and × close it (the caller
// goes back one history entry); the header renames, downloads and deletes.
export default function DocumentModal({
  doc,
  documents,
  ownerLabel,
  ownerNoun = "ticket",
  onClose,
  onRenamed,
  onDeleted,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  ownerLabel: string;
  ownerNoun?: string;
  onClose: () => void;
  onRenamed: (doc: DocumentMeta) => void;
  onDeleted: () => void;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const shown = displayName(doc);
  useEscape(onClose);

  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => api.documents.get(doc.id))
      .then((full) => {
        if (cancelled) return;
        setContent(full.content);
        setFailed(false);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.revision]);

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = dialogRef.current;
    if (e.key !== "Tab" || !root || deleting) return;
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

  let body: React.ReactNode;
  if (failed) body = <p className="text-sm text-slate-500">This document could not be loaded.</p>;
  else if (content === null) body = <p className="text-sm text-slate-600">Loading…</p>;
  else if (content === "") body = <p className="text-sm text-slate-600">This document is empty.</p>;
  else
    body = (
      <div data-testid="document-content" className="prose-card">
        <Markdown>{content}</Markdown>
      </div>
    );

  return (
    <div className="fixed inset-0 z-[60] flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={renaming ? undefined : titleId}
        aria-label={renaming ? shown : undefined}
        tabIndex={-1}
        onKeyDown={trapTab}
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
          <span className="hidden shrink-0 whitespace-nowrap text-xs text-slate-500 sm:inline">
            {formatSize(doc.size)} · updated {activityTime(doc.updatedAt)}
          </span>
          <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
            <Download className="h-4 w-4" />
          </a>
          <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(true)} className={ICON_BUTTON}>
            <Pencil className="h-4 w-4" />
          </button>
          <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(true)} className={`${ICON_BUTTON} hover:text-red-400`}>
            <Trash2 className="h-4 w-4" />
          </button>
          <button type="button" aria-label="Close document" title="Close" onClick={onClose} className={ICON_BUTTON}>
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-8 sm:py-6">{body}</div>
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
    </div>
  );
}
