import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Label } from "../api/client";

export default function LabelPicker({
  id,
  value,
  onChange,
}: {
  /** Applied to the add-label input, so an outer label's htmlFor can name it. */
  id?: string;
  value: string[];
  onChange: (names: string[]) => void;
}) {
  const [all, setAll] = useState<Label[]>([]);
  const [draft, setDraft] = useState("");

  useEffect(() => {
    api.labels.list().then(setAll).catch(() => setAll([]));
  }, []);

  const colorFor = (name: string) =>
    all.find((l) => l.name.toLowerCase() === name.toLowerCase())?.color ?? "#6B7280";

  const add = (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    if (value.some((v) => v.toLowerCase() === trimmed.toLowerCase())) {
      setDraft("");
      return;
    }
    onChange([...value, trimmed]);
    setDraft("");
  };

  const suggestions = all.filter(
    (l) =>
      l.name.toLowerCase().includes(draft.toLowerCase()) &&
      !value.some((v) => v.toLowerCase() === l.name.toLowerCase())
  );

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        {value.map((name) => (
          <span
            key={name}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium"
            style={{ backgroundColor: colorFor(name) + "1f", color: colorFor(name) }}
          >
            {name}
            <button
              type="button"
              onClick={() => onChange(value.filter((v) => v !== name))}
              className="opacity-60 hover:opacity-100"
              aria-label={`Remove ${name}`}
            >
              <X className="w-3 h-3" />
            </button>
          </span>
        ))}
      </div>
      <input
        id={id}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            add(draft);
          }
        }}
        placeholder="Add a label and press Enter"
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
      />
      {draft && suggestions.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {suggestions.slice(0, 6).map((l) => (
            <button
              key={l.id}
              type="button"
              onClick={() => add(l.name)}
              className="rounded px-1.5 py-0.5 text-[11px]"
              style={{ backgroundColor: l.color + "1f", color: l.color }}
            >
              {l.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
