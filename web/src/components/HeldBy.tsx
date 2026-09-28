// Who holds the ticket, first in the ticket page's side column, with the
// person's Stop work under it. It shows only while an agent holds the
// ticket. Stop work asks first, inline, saying what happens: the ticket goes
// back to To do at once, and the agent learns on its next call and can still
// leave its hand-off. The agent is shown, never edited: the person does not
// assign agents.
import { useEffect, useId, useRef, useState } from "react";
import type { Agent } from "../api/client";
import { VendorMark } from "./EntryCard";
import { useEscape } from "../lib/escapeStack";
import { heldDotColor, seenAgo } from "../lib/heldBy";

// How often "seen … ago" is worked out again while the page stays open.
const TICK_MS = 30_000;

export default function HeldBy({
  ticketKey,
  status,
  agent,
  onStop,
  now,
}: {
  ticketKey: string;
  status: string;
  agent: Agent | undefined;
  // Stops the work; the editor reports a failure on its own error strip.
  onStop: () => Promise<void>;
  // The time "seen … ago" counts to; the clock's own when left out.
  now?: number;
}) {
  const [confirming, setConfirming] = useState(false);
  const [clock, setClock] = useState(() => Date.now());
  const stopRef = useRef<HTMLButtonElement>(null);
  const labelId = useId();
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now()), TICK_MS);
    return () => clearInterval(timer);
  }, []);
  if (!agent) return null;

  // The session's last seen, the one staleness is judged by.
  const seen = seenAgo(agent.sessionLastSeenAt ?? agent.lastSeenAt, now ?? clock);
  const cancel = () => {
    setConfirming(false);
    stopRef.current?.focus();
  };
  return (
    <div className="space-y-2.5" data-testid="held-by">
      <div>
        <span id={labelId} className="mb-1.5 block text-xs font-medium text-slate-500">
          Held by
        </span>
        <div aria-labelledby={labelId} className="flex flex-wrap items-center gap-1.5 text-sm text-slate-300">
          <span aria-hidden="true" className={`h-2 w-2 shrink-0 rounded-full ${heldDotColor(status)}`} />
          <VendorMark provider={agent.provider} />
          <span>
            {agent.role}
            {agent.model && ` · ${agent.model}`}
            {seen && <span className="text-slate-500"> · {seen}</span>}
            {agent.stale && <span className="text-slate-500"> · stale</span>}
          </span>
        </div>
      </div>
      {/* It stays while the confirm is open, as the mock has it, so Keep
          working can give focus back to it. */}
      <button
        ref={stopRef}
        type="button"
        aria-expanded={confirming}
        onClick={() => setConfirming(true)}
        className="rounded-lg border border-red-500/50 px-3 py-1.5 text-sm font-semibold text-red-400 transition-colors hover:bg-red-500/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400"
      >
        Stop work
      </button>
      {confirming && (
        <StopConfirm
          ticketKey={ticketKey}
          role={agent.role}
          onCancel={cancel}
          onStop={async () => {
            try {
              await onStop();
            } finally {
              setConfirming(false);
            }
          }}
        />
      )}
    </div>
  );
}

// The inline confirm. Focus starts on the safe choice, and Escape keeps
// working, as the editor's discard confirm does.
function StopConfirm({
  ticketKey,
  role,
  onCancel,
  onStop,
}: {
  ticketKey: string;
  role: string;
  onCancel: () => void;
  onStop: () => Promise<void>;
}) {
  const [stopping, setStopping] = useState(false);
  const keepRef = useRef<HTMLButtonElement>(null);
  const descId = useId();
  useEscape(onCancel);
  useEffect(() => {
    keepRef.current?.focus();
  }, []);

  return (
    <div
      role="alertdialog"
      aria-label="Confirm stop work"
      aria-describedby={descId}
      className="space-y-2 rounded-lg border border-red-500/50 bg-red-500/10 px-3 py-2.5 text-[12.5px] text-slate-300"
    >
      <p id={descId}>
        Stop work on {ticketKey}? The ticket goes back to To do now. The {role || "agent"} is told on its next call, and
        can still leave its hand-off.
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={stopping}
          onClick={async () => {
            setStopping(true);
            try {
              await onStop();
            } finally {
              setStopping(false);
            }
          }}
          className="rounded-lg bg-red-700 px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-red-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 disabled:opacity-60"
        >
          {stopping ? "Stopping…" : "Stop work"}
        </button>
        <button
          ref={keepRef}
          type="button"
          onClick={onCancel}
          className="rounded-lg border border-slate-700 px-3 py-1.5 text-sm font-semibold text-slate-300 transition-colors hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400"
        >
          Keep working
        </button>
      </div>
    </div>
  );
}
