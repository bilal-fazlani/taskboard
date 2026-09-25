import { AlertTriangle, ArrowUp, ArrowRight, ArrowDown } from "lucide-react";

const PRIORITY_CONFIG: Record<string, { color: string; icon: typeof ArrowUp }> = {
  urgent: { color: "text-red-500", icon: AlertTriangle },
  high: { color: "text-orange-500", icon: ArrowUp },
  medium: { color: "text-yellow-500", icon: ArrowRight },
  low: { color: "text-green-500", icon: ArrowDown },
};

export default function PriorityBadge({ priority }: { priority: string }) {
  const config = PRIORITY_CONFIG[priority];
  if (!config) return null;
  const Icon = config.icon;
  return (
    <span className={`inline-flex items-center gap-1 text-xs ${config.color}`}>
      <Icon className="w-3 h-3" />
      {priority}
    </span>
  );
}

/** Just the priority's icon, in its colour, for places that write the name themselves. */
export function PriorityIcon({ priority, className = "" }: { priority: string; className?: string }) {
  const config = PRIORITY_CONFIG[priority];
  if (!config) return null;
  const Icon = config.icon;
  return <Icon aria-hidden="true" className={`w-3 h-3 shrink-0 ${config.color} ${className}`} />;
}
