import {
  FolderKanban,
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

// The three views show the same tickets three ways, so the sidebar groups them.
// Each page's heading uses the same label as its entry here.
export const VIEWS_GROUP_LABEL = "Views";

export const viewItems: NavItem[] = [
  { to: "/", icon: Workflow, label: "Dependencies" },
  { to: "/kanban", icon: LayoutDashboard, label: "Kanban" },
  { to: "/table", icon: Table, label: "Table" },
];

// Projects and labels are different things, so they sit outside the group.
export const otherItems: NavItem[] = [
  { to: "/projects", icon: FolderKanban, label: "Projects" },
  { to: "/labels", icon: Tag, label: "Labels" },
];
