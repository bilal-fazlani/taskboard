import { useState } from "react";
import { X } from "lucide-react";

/**
 * Chip editor for a ticket's repos. Unlike LabelPicker there is no registry to
 * suggest from: repos are free-form strings, and they are matched exactly, so
 * acme/API and acme/api are two different repos here as well as on the server.
 */
export default function RepoPicker({
  value,
  onChange,
}: {
  value: string[];
  onChange: (repos: string[]) => void;
}) {
  const [draft, setDraft] = useState("");

  const add = (repo: string) => {
    const trimmed = repo.trim();
    if (!trimmed) return;
    if (!value.includes(trimmed)) onChange([...value, trimmed]);
    setDraft("");
  };

  return (
    <div className="space-y-2">
      {value.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {value.map((repo) => (
            <span
              key={repo}
              className="inline-flex items-center gap-1 rounded bg-slate-800 px-1.5 py-0.5 text-[11px] font-mono text-slate-300"
            >
              {repo}
              <button
                type="button"
                onClick={() => onChange(value.filter((v) => v !== repo))}
                className="opacity-60 hover:opacity-100"
                aria-label={`Remove ${repo}`}
              >
                <X className="w-3 h-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      <input
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            add(draft);
          }
        }}
        onBlur={() => add(draft)}
        placeholder="acme/billing-web, then Enter"
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm font-mono text-slate-200 focus:outline-none focus:border-slate-600"
      />
    </div>
  );
}
