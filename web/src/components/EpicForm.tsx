import { useId, useRef, useState } from "react";
import type { Epic } from "../api/client";
import { nameError, serverMessage } from "../lib/epics";

const FIELD =
  "w-full bg-slate-800 border rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1";

/**
 * An epic's name and description, for New epic and the epic modal. The name
 * is checked as it is typed against the project's epics, with the store's
 * own wording; the button stays enabled, and pressing it while the name is
 * wrong keeps the form open on the field. A save the server refuses (another
 * tab took the name meanwhile) shows its message in the same place and keeps
 * what was typed.
 */
export default function EpicForm({
  epic,
  epics,
  onCancel,
  onSave,
}: {
  /** The epic being edited, or none for a new one. */
  epic?: Epic;
  /** The project's epics, which the name must differ from. */
  epics: readonly Epic[];
  onCancel: () => void;
  onSave: (data: { name: string; description: string }) => Promise<void>;
}) {
  const [name, setName] = useState(epic?.name ?? "");
  const [description, setDescription] = useState(epic?.description ?? "");
  // An empty name is only pointed out once there has been something to empty,
  // or a try to save it, not the moment a new epic's form opens.
  const [touched, setTouched] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const errorId = useId();
  const nameId = useId();
  const descriptionId = useId();

  const invalid = nameError(name, epics, epic?.id);
  const localError = invalid === "Enter a name" && !touched ? null : invalid;
  const error = serverError ?? localError;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setTouched(true);
    if (invalid) {
      setServerError(null);
      nameRef.current?.focus();
      return;
    }
    setSaving(true);
    try {
      await onSave({ name: name.trim(), description: description.trim() });
    } catch (err) {
      setServerError(serverMessage(err, "The epic was not saved."));
      nameRef.current?.focus();
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} noValidate className="space-y-4">
      <div>
        <label htmlFor={nameId} className="mb-1.5 block text-xs font-medium text-slate-400">
          Name
        </label>
        <input
          id={nameId}
          ref={nameRef}
          autoFocus
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setTouched(true);
            setServerError(null);
          }}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          className={`${FIELD} ${error ? "border-red-500/60 focus:ring-red-500" : "border-slate-700 focus:ring-blue-500"}`}
        />
        {error && (
          <p id={errorId} role="alert" className="mt-1.5 text-xs text-red-400">
            {error}
          </p>
        )}
      </div>
      <div>
        <label htmlFor={descriptionId} className="mb-1.5 block text-xs font-medium text-slate-400">
          Description <span className="font-normal text-slate-600">(optional)</span>
        </label>
        <input
          id={descriptionId}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="One line about what it groups"
          className={`${FIELD} border-slate-700 focus:ring-blue-500`}
        />
      </div>
      <div className="flex justify-end gap-3 pt-2">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 text-sm text-slate-400 transition-colors hover:text-white"
        >
          Cancel
        </button>
        <button
          type="submit"
          className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-500"
        >
          {epic ? "Save" : "Create"}
        </button>
      </div>
    </form>
  );
}
