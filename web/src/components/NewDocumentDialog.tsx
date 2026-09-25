import { useId, useState } from "react";
import { api, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { documentNameError } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// Asks for a new markdown document's name and creates it empty. The name
// rules are checked here first, in the server's words; a name taken
// meanwhile shows the server's message. Escape or a click beside it cancels.
export default function NewDocumentDialog({
  owner,
  documents,
  ownerNoun = "ticket",
  onCancel,
  onCreated,
}: {
  owner: DocumentOwnerRef;
  documents: readonly DocumentMeta[];
  ownerNoun?: string;
  onCancel: () => void;
  onCreated: (doc: DocumentMeta) => void;
}) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const ids = useId();
  useEscape(onCancel);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const local = documentNameError(name, documents, undefined, ownerNoun);
    if (local) {
      setError(local);
      return;
    }
    setSaving(true);
    try {
      onCreated(await api.documents.create({ ...owner, name: name.trim(), format: "markdown", content: "" }));
    } catch (err) {
      setError(serverMessage(err, "The document was not created."));
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center p-4">
      <div aria-hidden="true" className="absolute inset-0 bg-slate-950/60" onClick={onCancel} />
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${ids}-title`}
        onSubmit={submit}
        noValidate
        className="relative w-full max-w-sm space-y-4 rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">
          New document
        </h3>
        <div>
          <label htmlFor={`${ids}-name`} className="mb-1.5 block text-xs font-medium text-slate-400">
            Name
          </label>
          <div className="flex items-center gap-2">
            <input
              id={`${ids}-name`}
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setError(null);
              }}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? `${ids}-error` : undefined}
              placeholder="Rollout plan"
              className="min-w-0 flex-1 rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
            <span className="font-mono text-xs text-slate-500">.md</span>
          </div>
          {error && (
            <p id={`${ids}-error`} role="alert" className="mt-1.5 text-xs text-red-400">
              {error}
            </p>
          )}
        </div>
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm text-slate-400 hover:text-white">
            Cancel
          </button>
          <button
            type="submit"
            disabled={saving}
            className="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-60"
          >
            Create
          </button>
        </div>
      </form>
    </div>
  );
}
