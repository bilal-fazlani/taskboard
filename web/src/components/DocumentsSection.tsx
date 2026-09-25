import { useRef, useState } from "react";
import { Download, FileCode, FileText, Pencil, Plus, Trash2, Upload, X } from "lucide-react";
import { api, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { activityTime } from "../lib/activity";
import {
  displayName,
  formatSize,
  imageDimensions,
  isImageFormat,
  MAX_DOCUMENT_BYTES,
  nameFromFilename,
  tooLargeMessage,
  UPLOAD_ACCEPT,
} from "../lib/documents";
import { serverMessage } from "../lib/epics";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";
import NewDocumentDialog from "./NewDocumentDialog";

const SECTION_HEADING = "text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5";
const HEADER_BUTTON =
  "inline-flex items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 transition-colors hover:bg-slate-800 focus:outline-none focus:ring-1 focus:ring-blue-500";
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";

// A ticket's documents, in the order they were added: each opens in the
// document modal, and can be downloaded, renamed in place or deleted. New
// creates an empty markdown document by name; Upload adds a .md, .html or
// .htm file, or a .png, .jpg, .jpeg, .gif or .webp image, named from its
// filename (the extension sets the format), checking its type and size
// before reading it. HTML rows get a code icon; an image row shows its
// thumbnail, and its size in pixels where the others show when they changed.
export default function DocumentsSection({
  documents,
  failed,
  notice,
  onDismissNotice,
  onOpen,
  onChanged,
  owner,
  onCreated,
  ownerNoun = "ticket",
}: {
  documents: readonly DocumentMeta[] | null;
  failed: boolean;
  notice: string | null;
  onDismissNotice: () => void;
  onOpen: (doc: DocumentMeta) => void;
  /** A rename or delete went through; the owner reloads the list. */
  onChanged: () => void;
  owner: DocumentOwnerRef;
  /** New (edit = true, open it for editing) or Upload (edit = false) added one. */
  onCreated: (doc: DocumentMeta, edit: boolean) => void;
  ownerNoun?: string;
}) {
  const [renaming, setRenaming] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<DocumentMeta | null>(null);
  const [creating, setCreating] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const upload = async (file: File) => {
    setUploadError(null);
    const parsed = nameFromFilename(file.name);
    if ("error" in parsed) {
      setUploadError(parsed.error);
      return;
    }
    if (file.size > MAX_DOCUMENT_BYTES) {
      setUploadError(tooLargeMessage(file.size, parsed.format));
      return;
    }
    setUploading(true);
    try {
      if (isImageFormat(parsed.format)) {
        // The server reads the file itself, and names it from its filename.
        onCreated(await api.documents.createImage(owner, file), false);
        return;
      }
      const content = await file.text();
      onCreated(await api.documents.create({ ...owner, name: parsed.name, format: parsed.format, content }), false);
    } catch (err) {
      setUploadError(serverMessage(err, "The document was not uploaded."));
    } finally {
      setUploading(false);
    }
  };

  let body: React.ReactNode = null;
  if (failed) {
    body = <p className="text-sm text-slate-500">The documents could not be loaded.</p>;
  } else if (documents && documents.length === 0) {
    body = <p className="text-sm text-slate-600">No documents yet.</p>;
  } else if (documents) {
    body = (
      <ul className="divide-y divide-slate-800 rounded-lg border border-slate-800">
        {documents.map((doc) => {
          const shown = displayName(doc);
          return (
            <li key={doc.id} data-testid="document-row" className="flex items-center gap-2.5 px-3 py-2">
              {isImageFormat(doc.format) ? (
                <img
                  src={api.documents.thumbnailUrl(doc.id, doc.revision)}
                  alt=""
                  data-testid="document-thumbnail"
                  loading="lazy"
                  className="h-8 w-8 shrink-0 rounded border border-slate-700 bg-slate-800 object-cover"
                />
              ) : doc.format === "html" ? (
                <FileCode aria-hidden="true" data-testid="document-icon-html" className="h-4 w-4 shrink-0 text-slate-500" />
              ) : (
                <FileText aria-hidden="true" data-testid="document-icon-markdown" className="h-4 w-4 shrink-0 text-slate-500" />
              )}
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
                    {[formatSize(doc.size), isImageFormat(doc.format) ? imageDimensions(doc) : activityTime(doc.updatedAt)]
                      .filter(Boolean)
                      .join(" · ")}
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
      <div className="mb-2.5 flex items-center gap-2">
        <h3 className={`${SECTION_HEADING} mb-0 flex-1`}>Documents</h3>
        <button type="button" onClick={() => setCreating(true)} aria-label="New document" title="New document" className={HEADER_BUTTON}>
          <Plus aria-hidden="true" className="h-3 w-3" /> New
        </button>
        <button type="button" onClick={() => fileRef.current?.click()} disabled={uploading} title="Upload a .md, .html or .htm file, or a PNG, JPEG, GIF or WebP image" className={`${HEADER_BUTTON} disabled:opacity-60`}>
          <Upload aria-hidden="true" className="h-3 w-3" /> Upload
        </button>
        <input
          ref={fileRef}
          type="file"
          accept={UPLOAD_ACCEPT}
          aria-label="Upload a document"
          tabIndex={-1}
          className="sr-only"
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (file) void upload(file);
          }}
        />
      </div>
      {uploadError && (
        <div role="alert" className="mb-2 flex items-center gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs text-red-300">
          <span className="flex-1">{uploadError}</span>
          <button type="button" aria-label="Dismiss upload error" onClick={() => setUploadError(null)} className="text-red-300/70 hover:text-red-200">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
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
      {creating && (
        <NewDocumentDialog
          owner={owner}
          documents={documents ?? []}
          ownerNoun={ownerNoun}
          onCancel={() => setCreating(false)}
          onCreated={(doc) => {
            setCreating(false);
            onCreated(doc, true);
          }}
        />
      )}
    </div>
  );
}
