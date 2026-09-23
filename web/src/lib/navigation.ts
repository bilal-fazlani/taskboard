import {
  FolderKanban,
  Layers,
  LayoutDashboard,
  Tag,
  Table,
  Workflow,
  type LucideIcon,
} from "lucide-react";

export interface NavItem {
  to: string;
  icon: LucideIcon;
  label: string;
}

// The views show a project's tickets three ways, ticket by ticket, so the
// sidebar groups them together and carries the shared filters between them
// (see Layout). Each page's heading uses the same label as its entry here.
export const VIEWS_GROUP_LABEL = "Views";

export const viewItems: NavItem[] = [
  { to: "/", icon: Workflow, label: "Dependencies" },
  { to: "/kanban", icon: LayoutDashboard, label: "Kanban" },
  { to: "/table", icon: Table, label: "Table" },
];

// Projects, Epics and Labels sit outside the Views group and carry no
// filters. Epics shows a project's tickets grouped by epic rather than
// ticket by ticket, so it belongs at this level, not with the views above.
export const otherItems: NavItem[] = [
  { to: "/projects", icon: FolderKanban, label: "Projects" },
  { to: "/epics", icon: Layers, label: "Epics" },
  { to: "/labels", icon: Tag, label: "Labels" },
];
