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

// The views show a project's tickets: three ways ticket by ticket, and Epics
// grouped by epic, so the sidebar groups them. Each page's heading uses the
// same label as its entry here.
export const VIEWS_GROUP_LABEL = "Views";

export const viewItems: NavItem[] = [
  { to: "/", icon: Workflow, label: "Dependencies" },
  { to: "/kanban", icon: LayoutDashboard, label: "Kanban" },
  { to: "/table", icon: Table, label: "Table" },
  { to: "/epics", icon: Layers, label: "Epics" },
];

// Projects and labels are different things, so they sit outside the group.
export const otherItems: NavItem[] = [
  { to: "/projects", icon: FolderKanban, label: "Projects" },
  { to: "/labels", icon: Tag, label: "Labels" },
];
