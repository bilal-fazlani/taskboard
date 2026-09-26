import { useEffect, useState } from "react";
import { StickyNote, X } from "lucide-react";
import { api, type DependencyKind, type Ticket, type TicketRef } from "../api/client";
import { useEscape } from "../lib/escapeStack";

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

const KIND_LABEL: Record<DependencyKind, string> = {
  needs_work: "needs work",
  conflict_only: "conflict only",
};

function isConflictOnly(ref: { kind?: DependencyKind }): boolean {
  return ref.kind === "conflict_only";
}

// A dependency's kind as a small pill. With onToggle it is a button that
// switches the kind; without it, it is read only and shows only a
// conflict-only kind, since needing the work is what a dependency means by
// default.
export function KindPill({ ticketRef, onToggle }: { ticketRef: TicketRef; onToggle?: () => void }) {
  const conflict = isConflictOnly(ticketRef);
  const label = KIND_LABEL[conflict ? "conflict_only" : "needs_work"];
  const look = conflict
    ? "border-dashed border-slate-500 text-slate-300"
    : "border-slate-700 text-slate-500";
  const cls = `shrink-0 rounded-full border px-2 py-0.5 text-[10px] ${look}`;
  if (!onToggle) return conflict ? <span className={cls}>{label}</span> : null;
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={`Dependency on ${ticketRef.key}: ${label}. Switch to ${conflict ? KIND_LABEL.needs_work : KIND_LABEL.conflict_only}`}
      title={
        conflict
          ? "Waits only so the two don't change the same files. Click: needs its work"
          : "Needs its work. Click: waits only to avoid a conflict"
      }
      className={`${cls} hover:border-slate-400 hover:text-slate-200`}
    >
      {label}
    </button>
  );
}

// A dependency's note while it is being edited: Enter or leaving the field
// keeps the text, Escape puts back what was there.
function NoteField({ ticketRef, onDone }: { ticketRef: TicketRef; onDone: (note: string | null) => void }) {
  const [text, setText] = useState(ticketRef.note ?? "");
  useEscape(() => onDone(null));
  return (
    <input
      autoFocus
      value={text}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => onDone(text)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          onDone(text);
        }
      }}
      aria-label={`Note on ${ticketRef.key}`}
      placeholder="Why it waits, e.g. the files both change"
      className="mt-1 w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 text-[11px] text-slate-200 placeholder-slate-600 focus:outline-none focus:border-slate-600"
    />
  );
}

// One Depends on row: the ticket, its kind pill, its status and remove, with
// its note as a muted line underneath that a click edits.
function DependencyRow({
  ticketRef,
  onOpen,
  onChange,
  onRemove,
}: {
  ticketRef: TicketRef;
  onOpen?: (id: string) => void;
  onChange: (next: TicketRef) => void;
  onRemove: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const note = ticketRef.note ?? "";
  const finish = (text: string | null) => {
    setEditing(false);
    if (text === null) return;
    const trimmed = text.trim();
    if (trimmed !== note) onChange({ ...ticketRef, note: trimmed });
  };
  return (
    <div
      className={`rounded-md border bg-slate-900/60 px-2.5 py-1.5 ${
        isConflictOnly(ticketRef) ? "border-dashed border-slate-700" : "border-slate-800"
      }`}
    >
      <div className="flex items-center gap-2">
        <TicketRefLabel ticketRef={ticketRef} onOpen={onOpen} />
        <KindPill
          ticketRef={ticketRef}
          onToggle={() => onChange({ ...ticketRef, kind: isConflictOnly(ticketRef) ? "needs_work" : "conflict_only" })}
        />
        <span className="shrink-0 rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
          {ticketRef.status.replace("_", " ")}
        </span>
        {!note && !editing && (
          <button
            type="button"
            onClick={() => setEditing(true)}
            className="text-slate-600 hover:text-slate-300"
            aria-label={`Add a note on ${ticketRef.key}`}
            title="Add a note"
          >
            <StickyNote className="w-3.5 h-3.5" />
          </button>
        )}
        <button
          type="button"
          onClick={onRemove}
          className="text-slate-600 hover:text-red-400"
          aria-label={`Remove dependency ${ticketRef.key}`}
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
      {editing ? (
        <NoteField ticketRef={ticketRef} onDone={finish} />
      ) : (
        note && (
          <button
            type="button"
            onClick={() => setEditing(true)}
            aria-label={`Edit note on ${ticketRef.key}: ${note}`}
            title="Edit note"
            className="mt-0.5 block w-full truncate pl-[60px] text-left text-[11px] text-slate-500 hover:text-slate-300"
          >
            {note}
          </button>
        )
      )}
    </div>
  );
}

// A search box over every project's tickets (links may cross projects) and
// the first six matches, leaving out `exclude`. A pick clears the box.
function TicketSearch({
  all,
  exclude,
  placeholder,
  onPick,
  onFocus,
}: {
  all: Ticket[];
  exclude: (t: Ticket) => boolean;
  placeholder: string;
  onPick: (t: Ticket) => void;
  onFocus?: () => void;
}) {
  const [query, setQuery] = useState("");
  const matches = query.trim()
    ? all
        .filter((t) => !exclude(t))
        .filter((t) => {
          const key = `${t.projectPrefix}-${t.number}`.toLowerCase();
          const q = query.toLowerCase();
          return key.includes(q) || t.title.toLowerCase().includes(q);
        })
        .slice(0, 6)
    : [];
  return (
    <>
      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onFocus={onFocus}
        placeholder={placeholder}
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
      />
      {matches.length > 0 && (
        <div className="rounded-md border border-slate-800 divide-y divide-slate-800">
          {matches.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                onPick(t);
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
    </>
  );
}

function refOf(t: Ticket): TicketRef {
  return { id: t.id, key: `${t.projectPrefix}-${t.number}`, title: t.title, status: t.status };
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

  useEffect(() => {
    api.tickets.list().then(setAll).catch(() => setAll([]));
  }, []);

  return (
    <div className="space-y-2">
      {value.map((ref) => (
        <DependencyRow
          key={ref.id}
          ticketRef={ref}
          onOpen={onOpen}
          onChange={(next) => onChange(value.map((v) => (v.id === ref.id ? next : v)))}
          onRemove={() => onChange(value.filter((v) => v.id !== ref.id))}
        />
      ))}
      <TicketSearch
        all={all}
        exclude={(t) => t.id === excludeTicketId || value.some((v) => v.id === t.id)}
        placeholder="Search tickets to add..."
        onPick={(t) => onChange([...value, { ...refOf(t), kind: "needs_work" }])}
      />
    </div>
  );
}

// The ticket this one was surfaced from: a row to open or remove it, or a
// search to pick one. It loads the tickets to search only once the box is
// focused, since most tickets never set it.
export function SurfacedFromPicker({
  value,
  onChange,
  excludeTicketId,
  onOpen,
}: {
  value: TicketRef | null;
  onChange: (ref: TicketRef | null) => void;
  excludeTicketId: string;
  onOpen?: (id: string) => void;
}) {
  const [all, setAll] = useState<Ticket[] | null>(null);
  const load = () => {
    if (all !== null) return;
    setAll([]);
    api.tickets.list().then(setAll).catch(() => setAll([]));
  };

  if (value) {
    return (
      <div className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5">
        <TicketRefLabel ticketRef={value} onOpen={onOpen} />
        <span className="shrink-0 rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
          {value.status.replace("_", " ")}
        </span>
        <button
          type="button"
          onClick={() => onChange(null)}
          className="text-slate-600 hover:text-red-400"
          aria-label={`Remove surfaced from ${value.key}`}
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
    );
  }
  return (
    <div className="space-y-2">
      <TicketSearch
        all={all ?? []}
        exclude={(t) => t.id === excludeTicketId}
        placeholder="Search the ticket this was found during..."
        onPick={(t) => onChange(refOf(t))}
        onFocus={load}
      />
    </div>
  );
}
