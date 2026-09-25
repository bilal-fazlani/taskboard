import { useEffect, useId, useRef } from "react";
import { useEscape } from "../lib/escapeStack";

// "Discard your changes?" over whatever holds unsaved text (`what`, as in
// "this document"). Focus starts on the safe choice; a click beside the box
// cancels, and so does Escape: the question is the topmost layer on the
// escape stack while it is open, so Escape never reaches what is under it.
export default function DiscardChangesConfirm({
  what,
  onCancel,
  onDiscard,
}: {
  what: string;
  onCancel: () => void;
  onDiscard: () => void;
}) {
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
          Your edits to {what} have not been saved.
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
