import { useEffect, useRef, useState } from "react";
import { Pencil, Trash2 } from "lucide-react";
import { api, type Label } from "../api/client";

export default function Labels() {
  const [labels, setLabels] = useState<Label[]>([]);
  const [name, setName] = useState("");
  const [color, setColor] = useState("#6B7280");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  // Guards against the duplicate PUT that Enter would otherwise cause:
  // committing exits edit mode, which unmounts the still-focused <input>,
  // which fires a native blur that React redelivers to onBlur before this
  // function's own request has settled. While a commit for a given label
  // id is in flight, a re-entrant call for that same id is a no-op.
  const savingLabelId = useRef<string | null>(null);

  const load = () => api.labels.list().then(setLabels).catch(() => setLabels([]));
  useEffect(() => {
    load();
  }, []);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    await api.labels.create({ name: name.trim(), color });
    setName("");
    setColor("#6B7280");
    load();
  };

  const remove = async (l: Label) => {
    const suffix =
      l.ticketCount > 0
        ? ` It will be removed from ${l.ticketCount} ticket${l.ticketCount === 1 ? "" : "s"}.`
        : "";
    if (!confirm(`Delete the label "${l.name}"?${suffix}`)) return;
    await api.labels.delete(l.id);
    load();
  };

  const saveEdit = async (l: Label) => {
    if (savingLabelId.current === l.id) return;
    savingLabelId.current = l.id;
    // Exit edit mode before the request so the resulting unmount (and its
    // cascading blur) happens while the guard above is still set.
    setEditingId(null);
    try {
      if (editName.trim() && editName !== l.name) {
        await api.labels.update(l.id, { name: editName.trim() });
      }
      load();
    } finally {
      savingLabelId.current = null;
    }
  };

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-xl font-semibold text-white mb-6">Labels</h1>

      <form onSubmit={create} className="flex gap-2 mb-6">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Label name"
          className="flex-1 bg-slate-800 border border-slate-700 rounded-md px-3 py-2 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
        />
        <input
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          className="w-12 h-10 bg-slate-800 border border-slate-700 rounded-md cursor-pointer"
          aria-label="Label color"
        />
        <button
          type="submit"
          className="bg-blue-600 hover:bg-blue-500 text-white text-sm px-4 rounded-md transition-colors"
        >
          Create
        </button>
      </form>

      <div className="border border-slate-800 rounded-lg bg-slate-900 divide-y divide-slate-800">
        {labels.length === 0 && (
          <p className="px-4 py-6 text-sm text-slate-500">No labels yet.</p>
        )}
        {labels.map((l) => (
          <div key={l.id} className="flex items-center gap-3 px-4 py-3">
            {editingId === l.id ? (
              <input
                autoFocus
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                onBlur={() => saveEdit(l)}
                onKeyDown={(e) => e.key === "Enter" && saveEdit(l)}
                className="bg-slate-800 border border-slate-700 rounded px-2 py-0.5 text-sm text-slate-200"
              />
            ) : (
              <span
                className="inline-flex items-center rounded px-2 py-0.5 text-[11px] font-medium"
                style={{ backgroundColor: l.color + "1f", color: l.color }}
              >
                {l.name}
              </span>
            )}
            <span className="flex-1 text-xs text-slate-500">
              {l.ticketCount} ticket{l.ticketCount === 1 ? "" : "s"}
            </span>
            <button
              onClick={() => {
                setEditingId(l.id);
                setEditName(l.name);
              }}
              className="text-slate-600 hover:text-slate-300"
              aria-label={`Rename ${l.name}`}
            >
              <Pencil className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => remove(l)}
              className="text-slate-600 hover:text-red-400"
              aria-label={`Delete ${l.name}`}
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
