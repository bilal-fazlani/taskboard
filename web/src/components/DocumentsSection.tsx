import { useState } from "react";
import { Download, FileText, Pencil, Trash2, X } from "lucide-react";
import { api, type DocumentMeta } from "../api/client";
import { activityTime } from "../lib/activity";
import { displayName, formatSize } from "../lib/documents";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const SECTION_HEADING = "text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5";
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";

// A ticket's documents, in the order they were added: each opens in the
// document modal, and can be downloaded, renamed in place or deleted.
export default function DocumentsSection({
  documents,
  failed,
  notice,
  onDismissNotice,
  onOpen,
  onChanged,
  ownerNoun = "ticket",
}: {
  documents: readonly DocumentMeta[] | null;
  failed: boolean;
  notice: string | null;
  onDismissNotice: () => void;
  onOpen: (doc: DocumentMeta) => void;
  /** A rename or delete went through; the owner reloads the list. */
  onChanged: () => void;
  ownerNoun?: string;
}) {
  const [renaming, setRenaming] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<DocumentMeta | null>(null);

  let body: React.ReactNode = null;
  if (failed) {
    body = <p className="text-sm text-slate-500">The documents could not be loaded.</p>;
  } else if (documents && documents.length === 0) {
    body = <p className="text-sm text-slate-600">No documents yet. Agents can attach them.</p>;
  } else if (documents) {
    body = (
      <ul className="divide-y divide-slate-800 rounded-lg border border-slate-800">
        {documents.map((doc) => {
          const shown = displayName(doc);
          return (
            <li key={doc.id} data-testid="document-row" className="flex items-center gap-2.5 px-3 py-2">
              <FileText aria-hidden="true" className="h-4 w-4 shrink-0 text-slate-500" />
              {renaming === doc.id ? (
                <DocumentRenameField
                  doc={doc}
                  documents={documents}
                  ownerNoun={ownerNoun}
                  onCancel={() => setRenaming(null)}
                  onRenamed={() => {
                    setRenaming(null);
                    onChanged();
                  }}
                />
              ) : (
                <>
                  <button
                    type="button"
                    onClick={() => onOpen(doc)}
                    className="min-w-0 flex-1 truncate text-left text-sm text-blue-400 hover:underline focus:underline focus:outline-none"
                  >
                    {shown}
                  </button>
                  <span className="shrink-0 whitespace-nowrap text-xs text-slate-500">
                    {formatSize(doc.size)} · {activityTime(doc.updatedAt)}
                  </span>
                  <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
                    <Download className="h-3.5 w-3.5" />
                  </a>
                  <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(doc.id)} className={ICON_BUTTON}>
                    <Pencil className="h-3.5 w-3.5" />
                  </button>
                  <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(doc)} className={`${ICON_BUTTON} hover:text-red-400`}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </>
              )}
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <div>
      <h3 className={SECTION_HEADING}>Documents</h3>
      {notice && (
        <div role="status" className="mb-2 flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200">
          <span className="flex-1">{notice}</span>
          <button type="button" aria-label="Dismiss" onClick={onDismissNotice} className="text-amber-200/70 hover:text-amber-100">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
      {body}
      {deleting && (
        <DeleteDocumentConfirm
          doc={deleting}
          onCancel={() => setDeleting(null)}
          onDeleted={() => {
            setDeleting(null);
            onChanged();
          }}
        />
      )}
    </div>
  );
}
