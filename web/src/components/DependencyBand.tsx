import type { TicketRef } from "../api/client";
import { isDone } from "../lib/status";

const MAX_KEYS = 3;

/**
 * The dependency band on a board card. Red while any dependency is unfinished,
 * neutral once all are done, absent when there are none. Deliberately a
 * full-width band rather than a pill, so it never reads as a label chip.
 */
export default function DependencyBand({ dependsOn }: { dependsOn?: TicketRef[] }) {
  if (!dependsOn || dependsOn.length === 0) return null;

  const outstanding = dependsOn.some((d) => !isDone(d.status));
  const shown = dependsOn.slice(0, MAX_KEYS);
  const overflow = dependsOn.length - shown.length;

  return (
    <div
      title={dependsOn.map((d) => `${d.key} ${d.title} (${d.status})`).join("\n")}
      className={`flex items-center gap-1.5 rounded-r-md border-l-2 px-2 py-1 text-[10.5px] ${
        outstanding
          ? "border-red-500 bg-red-500/[0.08] text-red-300"
          : "border-slate-600 bg-slate-800/40 text-slate-500"
      }`}
    >
      <span>depends on</span>
      {shown.map((d, i) => (
        <span key={d.id} className="font-mono">
          {d.key}
          {i < shown.length - 1 ? "," : ""}
        </span>
      ))}
      {overflow > 0 && <span className="font-mono">+{overflow}</span>}
    </div>
  );
}
