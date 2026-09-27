// One entry as a card: its text, then a line saying what it is and who wrote
// it (type, role, model and a small vendor mark, or "You" for the person),
// the session chip, when, and the quiet Challenge action. Entries it
// replaced sit under it, folded, struck through when opened. Shared by every
// page that shows entries.
import { useEffect, useState, type ReactNode } from "react";
import { Check } from "lucide-react";
import type { AgentSession, Entry, EntryAgent } from "../api/client";
import { activityTime } from "../lib/activity";
import { entryExcerpt, entryTypeLabel, shortResumeCommand, splitRejected } from "../lib/entries";

const TYPE_STYLES: Record<string, string> = {
  decision: "bg-blue-500/10 text-blue-400",
  learning: "bg-teal-400/10 text-teal-400",
  hand_off: "bg-slate-400/15 text-slate-300",
  proof: "bg-green-400/10 text-green-400",
  review: "bg-violet-500/15 text-violet-400",
  note: "bg-pink-400/10 text-pink-400",
};

/** An entry's type as a small tag. `label` overrides the type's own name ("Review 2"). */
export function EntryTypeTag({ type, label }: { type: string; label?: string }) {
  return (
    <span
      data-testid="entry-type"
      className={`rounded px-1.5 py-px text-[10.5px] font-semibold uppercase tracking-wide ${
        TYPE_STYLES[type] ?? "bg-slate-400/15 text-slate-300"
      }`}
    >
      {label ?? entryTypeLabel(type)}
    </span>
  );
}

const PROVIDERS: Record<string, { name: string; letter: string; style: string }> = {
  anthropic: { name: "Anthropic", letter: "A", style: "bg-[#d97757] text-[#1a0f0a]" },
  openai: { name: "OpenAI", letter: "O", style: "bg-[#10a37f] text-white" },
  google: { name: "Google", letter: "G", style: "bg-[#4285f4] text-white" },
};

/** The model's provider as a small letter mark, secondary to the agent's name. */
export function VendorMark({ provider }: { provider: string }) {
  const known = PROVIDERS[provider];
  const name = known?.name ?? (provider && provider !== "other" ? provider : "Other provider");
  return (
    <span
      role="img"
      aria-label={name}
      title={name}
      data-provider={provider}
      className={`inline-grid h-[15px] w-[15px] shrink-0 place-items-center rounded text-[9px] font-extrabold leading-none ${
        known?.style ?? "bg-slate-600 text-slate-200"
      }`}
    >
      {known?.letter ?? "·"}
    </span>
  );
}

/** An agent as the author line names it: vendor mark, then role · model. */
export function AgentName({ agent }: { agent: EntryAgent | undefined }) {
  if (!agent) return <span className="text-slate-400">an agent</span>;
  return (
    <span className="inline-flex items-center gap-1.5">
      <VendorMark provider={agent.provider} />
      <span className="text-slate-300">
        {agent.role}
        {agent.model && ` · ${agent.model}`}
      </span>
    </span>
  );
}

/**
 * The session an entry came from, as its resume command: a click copies the
 * whole command, and hovering shows the machine it ran on.
 */
export function SessionChip({ session }: { session: AgentSession }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  if (!session.resumeCommand) return null;
  const copy = () => {
    navigator.clipboard
      ?.writeText(session.resumeCommand)
      .then(() => setCopied(true))
      .catch(() => {
        // Clipboard access can be refused; the title still shows the command.
      });
  };
  const machine = session.machine || "unknown machine";
  return (
    <>
      <button
        type="button"
        onClick={copy}
        title={`${machine} · click to copy: ${session.resumeCommand}`}
        aria-label={copied ? "Resume command copied" : `Copy resume command (${machine})`}
        data-testid="session-chip"
        className="inline-flex max-w-[16rem] items-center gap-1 truncate rounded border border-slate-700 bg-slate-800 px-1.5 py-px font-mono text-[11.5px] text-slate-400 transition-colors hover:border-slate-600 hover:text-slate-200"
      >
        <span className="truncate">{shortResumeCommand(session.resumeCommand)}</span>
        {copied ? <Check className="h-3 w-3 shrink-0 text-green-500" /> : <span className="opacity-70">⧉</span>}
      </button>
      {/* Always rendered, so a screen reader hears the copy when it lands. */}
      <span role="status" aria-live="polite" className="sr-only">
        {copied ? "Resume command copied" : ""}
      </span>
    </>
  );
}

/**
 * Who wrote an entry: "You" for the person; for an agent, its name and
 * session. A decision whose source is the person says so: "from you,
 * recorded by" the agent.
 */
export function EntryAuthor({ entry, agents }: { entry: Entry; agents: Record<string, EntryAgent> }) {
  if (!entry.agentId) {
    return <span className="text-pink-400">You</span>;
  }
  const agent = agents[entry.agentId];
  return (
    <>
      {entry.type === "decision" && entry.source === "person" ? (
        <span className="inline-flex flex-wrap items-center gap-1.5">
          <span>
            <span className="text-pink-400">from you</span>, recorded by
          </span>
          <AgentName agent={agent} />
        </span>
      ) : (
        <AgentName agent={agent} />
      )}
      {agent && <SessionChip session={agent.session} />}
    </>
  );
}

/** The entry's time, as the Activity list words it, with the full time on hover. */
export function EntryTime({ at, now }: { at: string; now?: Date }) {
  return <span title={new Date(at).toLocaleString()}>{activityTime(at, now)}</span>;
}

/** The entry's text; a decision's "Rejected: …" set apart under what was chosen. */
function EntryText({ entry, className = "text-slate-200" }: { entry: Entry; className?: string }) {
  if (entry.type === "decision") {
    const { chosen, rejected } = splitRejected(entry.text);
    return (
      <>
        <div data-testid="entry-text" className={`whitespace-pre-wrap text-sm ${className}`}>
          {chosen}
        </div>
        {rejected && <div className="mt-0.5 whitespace-pre-wrap text-[13px] text-slate-400">{rejected}</div>}
      </>
    );
  }
  return (
    <div data-testid="entry-text" className={`whitespace-pre-wrap text-sm ${className}`}>
      {entry.text}
    </div>
  );
}

/** The entries one replaced, folded under it; opened, each is struck through. */
function ReplacedEntries({
  replaced,
  agents,
  replacedAt,
  now,
}: {
  replaced: Entry[];
  agents: Record<string, EntryAgent>;
  replacedAt: (e: Entry) => string;
  now?: Date;
}) {
  const [open, setOpen] = useState(false);
  const n = replaced.length;
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="mt-2 text-[12.5px] text-slate-500 transition-colors hover:text-slate-300"
      >
        {open ? "▾" : "▸"} Replaces {n} earlier {n === 1 ? "entry" : "entries"}
      </button>
      {open &&
        replaced.map((old) => (
          <div
            key={old.id}
            data-testid="replaced-entry"
            className="mt-1.5 rounded-lg border border-dashed border-slate-800 px-3 py-2 opacity-55"
          >
            <div data-testid="entry-text" className="whitespace-pre-wrap text-sm text-slate-300 line-through decoration-slate-600">
              {old.text}
            </div>
            <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-slate-500">
              <EntryTypeTag type={old.type} />
              {old.agentId ? <span>{agents[old.agentId]?.role ?? "an agent"}</span> : <span>You</span>}
              <span>
                · replaced <EntryTime at={replacedAt(old)} now={now} />
              </span>
            </div>
          </div>
        ))}
    </div>
  );
}

/**
 * One entry as a card. `head` goes above the text (a challenge's pointer, a
 * review's verdict), `meta` joins the author line, and onChallenge, when
 * given, adds the quiet Challenge action.
 */
export default function EntryCard({
  entry,
  agents,
  replaced = [],
  typeLabel,
  challenged = false,
  tone = "default",
  head,
  meta,
  onChallenge,
  now,
}: {
  entry: Entry;
  agents: Record<string, EntryAgent>;
  replaced?: Entry[];
  typeLabel?: string;
  challenged?: boolean;
  tone?: "default" | "handoff" | "open-note" | "muted";
  head?: ReactNode;
  meta?: ReactNode;
  onChallenge?: (entry: Entry) => void;
  now?: Date;
}) {
  const toneStyle =
    tone === "handoff"
      ? "border-slate-700 bg-slate-800/55"
      : tone === "open-note"
        ? "border-pink-400/45 bg-pink-400/[0.06]"
        : challenged
          ? "border-pink-400/35 bg-slate-900/60"
          : "border-slate-800 bg-slate-900/60";
  // Each replaced entry went out when the one after it came in: the entry
  // itself for the first, then each replaced entry for the one it replaced.
  const replacedAt = (old: Entry) => {
    const i = replaced.indexOf(old);
    return (i === 0 ? entry : replaced[i - 1]).createdAt;
  };
  return (
    <article
      data-testid="entry"
      data-entry-id={entry.id}
      data-challenged={challenged || undefined}
      className={`rounded-lg border px-3 py-2.5 ${toneStyle} ${tone === "muted" ? "opacity-70" : ""}`}
    >
      {head}
      <EntryText entry={entry} />
      <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-500">
        <EntryTypeTag type={entry.type} label={typeLabel} />
        <EntryAuthor entry={entry} agents={agents} />
        <EntryTime at={entry.createdAt} now={now} />
        {meta}
        {challenged && <span className="text-[11px] text-pink-400">challenged by you</span>}
        {onChallenge && (
          <button
            type="button"
            onClick={() => onChallenge(entry)}
            // Every card has one, so the name says which entry it challenges.
            aria-label={`Challenge ${entryTypeLabel(entry.type).toLowerCase()}: “${entryExcerpt(entry.text, 40)}”`}
            className="ml-auto text-xs text-slate-500 transition-colors hover:text-slate-300"
          >
            Challenge
          </button>
        )}
      </div>
      {replaced.length > 0 && <ReplacedEntries replaced={replaced} agents={agents} replacedAt={replacedAt} now={now} />}
    </article>
  );
}
