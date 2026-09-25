import { useEffect, useId, useRef, useState } from "react";
import { api, type DocumentMeta } from "../api/client";
import { displayName } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// "Delete Design spec.md?" over everything else. Focus starts on Cancel; a
// click beside the box or Escape cancels. Deleting is permanent.
export default function DeleteDocumentConfirm({
  doc,
  onCancel,
  onDeleted,
}: {
  doc: DocumentMeta;
  onCancel: () => void;
  onDeleted: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const ids = useId();
  useEscape(onCancel);
  useEffect(() => cancelRef.current?.focus(), []);

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await api.documents.delete(doc.id);
      onDeleted();
    } catch (err) {
      setError(serverMessage(err, "The document was not deleted."));
      setBusy(false);
    }
  };

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
          Delete {displayName(doc)}?
        </h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">
          This can't be undone.
        </p>
        {error && (
          <p role="alert" className="mt-3 text-xs text-red-400">
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={confirm}
            disabled={busy}
            className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400 disabled:opacity-60"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
