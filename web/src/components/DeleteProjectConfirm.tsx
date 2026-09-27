import { useEffect, useId, useRef, useState } from "react";
import { api, type Project } from "../api/client";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// "Delete Billing?" over the Projects page, as DeleteDocumentConfirm asks
// before deleting a document. Focus starts on Cancel; a click beside the box
// or Escape cancels. Deleting a project takes it, its tickets, epics and
// documents off the board and out of every API, MCP and CLI read, and
// nothing brings it back. A delete that fails keeps the box open with the
// server's reason.
export default function DeleteProjectConfirm({
  project,
  onCancel,
  onDeleted,
}: {
  project: Pick<Project, "id" | "name" | "prefix">;
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
    setError(null);
    try {
      await api.projects.delete(project.id);
      onDeleted();
    } catch (err) {
      setError(serverMessage(err, "The project was not deleted."));
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
        aria-busy={busy || undefined}
        className="relative w-full max-w-sm rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">
          Delete {project.name}?
        </h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">
          {project.prefix} and all its tickets, epics and documents disappear from the board, and from the API, MCP
          and CLI. This can't be undone.
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
