import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Ticket, type TicketRef } from "../api/client";

// A linked ticket's key and title: a button that opens it when there is
// somewhere to open it, plain text otherwise.
export function TicketRefLabel({ ticketRef, onOpen }: { ticketRef: TicketRef; onOpen?: (id: string) => void }) {
  const label = (
    <>
      <span className="font-mono text-[11px] text-slate-400 min-w-[52px]">{ticketRef.key}</span>
      <span className="flex-1 text-[12.5px] text-slate-300 truncate group-hover:text-blue-300 group-hover:underline">
        {ticketRef.title}
      </span>
    </>
  );
  if (!onOpen) return label;
  return (
    <button
      type="button"
      onClick={() => onOpen(ticketRef.id)}
      aria-label={`Open ${ticketRef.key}: ${ticketRef.title}`}
      title={`Open ${ticketRef.key}`}
      className="group flex min-w-0 flex-1 items-center gap-2 text-left"
    >
      {label}
    </button>
  );
}

export default function DependencyPicker({
  value,
  onChange,
  excludeTicketId,
  onOpen,
}: {
  value: TicketRef[];
  onChange: (refs: TicketRef[]) => void;
  excludeTicketId: string;
  // Opens a dependency by id; without it the rows are plain text.
  onOpen?: (id: string) => void;
}) {
  const [all, setAll] = useState<Ticket[]>([]);
  const [query, setQuery] = useState("");

  useEffect(() => {
    api.tickets.list().then(setAll).catch(() => setAll([]));
  }, []);

  // Searches every project on purpose: dependencies may cross projects.
  const matches = query.trim()
    ? all
        .filter((t) => t.id !== excludeTicketId)
        .filter((t) => !value.some((v) => v.id === t.id))
        .filter((t) => {
          const key = `${t.projectPrefix}-${t.number}`.toLowerCase();
          const q = query.toLowerCase();
          return key.includes(q) || t.title.toLowerCase().includes(q);
        })
        .slice(0, 6)
    : [];

  return (
    <div className="space-y-2">
      {value.map((ref) => (
        <div
          key={ref.id}
          className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5"
        >
          <TicketRefLabel ticketRef={ref} onOpen={onOpen} />
          <span className="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
            {ref.status.replace("_", " ")}
          </span>
          <button
            type="button"
            onClick={() => onChange(value.filter((v) => v.id !== ref.id))}
            className="text-slate-600 hover:text-red-400"
            aria-label={`Remove dependency ${ref.key}`}
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      ))}
      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search tickets to add..."
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
      />
      {matches.length > 0 && (
        <div className="rounded-md border border-slate-800 divide-y divide-slate-800">
          {matches.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                onChange([
                  ...value,
                  {
                    id: t.id,
                    key: `${t.projectPrefix}-${t.number}`,
                    title: t.title,
                    status: t.status,
                  },
                ]);
                setQuery("");
              }}
              className="w-full flex items-center gap-2 px-2.5 py-1.5 text-left hover:bg-slate-800"
            >
              <span className="font-mono text-[11px] text-slate-400 min-w-[52px]">
                {t.projectPrefix}-{t.number}
              </span>
              <span className="flex-1 text-[12.5px] text-slate-300 truncate">{t.title}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
