import { Loader2, X } from "lucide-react";
import type { ImageUpload, ImageUploadProblem } from "../hooks/usePasteImages";

// The small lines under a text editor that images were pasted or dropped
// into (usePasteImages): one per upload under way, and why any was not
// added, until dismissed or the next paste or drop. The upload lines sit in
// one live region that is always there, so each new line is announced, and
// so is each image once it is added (`added`, for screen readers only).
export default function ImageUploadStatus({
  uploads,
  problems,
  added = null,
  onDismiss,
}: {
  uploads: readonly ImageUpload[];
  problems: readonly ImageUploadProblem[];
  added?: string | null;
  onDismiss: () => void;
}) {
  return (
    <div className={uploads.length > 0 || problems.length > 0 ? "mt-1.5 space-y-1 text-xs" : "text-xs"}>
      <div aria-live="polite" data-testid="image-uploads" className="space-y-1">
        {uploads.map((u) => (
          <p key={u.id} data-testid="image-upload" className="flex items-center gap-1.5 text-slate-400">
            <Loader2 aria-hidden="true" className="h-3 w-3 shrink-0 animate-spin" />
            <span>Uploading {u.shown}… it appears in Documents when done</span>
          </p>
        ))}
        {added && uploads.length === 0 && <span className="sr-only">{added} was added.</span>}
      </div>
      {problems.length > 0 && (
        <div role="alert" className="flex items-start gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-2.5 py-1.5 text-red-300">
          <div className="flex-1 space-y-0.5">
            {problems.map((p) => (
              <p key={p.id}>{p.text}</p>
            ))}
          </div>
          <button type="button" aria-label="Dismiss image message" onClick={onDismiss} className="text-red-300/70 hover:text-red-200">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  );
}
