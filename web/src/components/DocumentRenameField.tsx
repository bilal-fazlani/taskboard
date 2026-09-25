import { useId, useState } from "react";
import { api, type DocumentMeta } from "../api/client";
import { documentNameError, extensionFor } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// Renames a document in place: the name without its extension, which shows
// beside the field and cannot be edited. The rules are checked here first,
// in the server's words, then by the server. Escape or Cancel leaves it.
export default function DocumentRenameField({
  doc,
  documents,
  ownerNoun = "ticket",
  onCancel,
  onRenamed,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  ownerNoun?: string;
  onCancel: () => void;
  onRenamed: (doc: DocumentMeta) => void;
}) {
  const [value, setValue] = useState(doc.name);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const errorId = useId();
  useEscape(onCancel);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const local = documentNameError(value, documents, doc.id, ownerNoun);
    if (local) {
      setError(local);
      return;
    }
    const name = value.trim();
    if (name === doc.name) {
      onCancel();
      return;
    }
    setSaving(true);
    try {
      onRenamed(await api.documents.update(doc.id, { name }));
    } catch (err) {
      setError(serverMessage(err, "The document was not renamed."));
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} noValidate className="flex min-w-0 flex-1 flex-col gap-1">
      <div className="flex items-center gap-2">
        <input
          aria-label="Document name"
          autoFocus
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setError(null);
          }}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          className={`min-w-0 flex-1 rounded-md border bg-slate-800 px-2 py-1 text-sm text-white focus:outline-none focus:ring-1 ${
            error ? "border-red-500/60 focus:ring-red-500" : "border-slate-700 focus:ring-blue-500"
          }`}
        />
        <span className="shrink-0 font-mono text-xs text-slate-500">{extensionFor(doc.format)}</span>
        <button
          type="submit"
          disabled={saving}
          className="shrink-0 rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-500 disabled:opacity-60"
        >
          Save
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="shrink-0 rounded-md px-2 py-1 text-xs text-slate-400 hover:text-white"
        >
          Cancel
        </button>
      </div>
      {error && (
        <p id={errorId} role="alert" className="text-xs text-red-400">
          {error}
        </p>
      )}
    </form>
  );
}
