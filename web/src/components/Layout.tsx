import { useEffect } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { Zap } from "lucide-react";
import { VIEWS_GROUP_LABEL, isCurrentPath, otherItems, viewItems, type NavItem } from "../lib/navigation";
import { filterSearch } from "../lib/filters";
import { rememberView } from "../lib/lastView";

// `search` is the query string the link carries; the views pass on the filters.
function NavEntry({ item, search = "" }: { item: NavItem; search?: string }) {
  const current = isCurrentPath(useLocation().pathname, item.to);
  return (
    <Link
      to={{ pathname: item.to, search }}
      aria-current={current ? "page" : undefined}
      className={`flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors ${
        current
          ? "bg-blue-500/15 text-blue-400"
          : "text-slate-400 hover:text-slate-200 hover:bg-slate-800"
      }`}
    >
      <item.icon className="w-4 h-4" />
      {item.label}
    </Link>
  );
}

export default function Layout() {
  const location = useLocation();
  const viewSearch = filterSearch(location.search);

  // The ticket view shown is the one an epic's row on the Epics view opens.
  useEffect(() => rememberView(location.pathname), [location.pathname]);

  return (
    <div className="flex h-screen overflow-hidden">
      <aside className="w-56 shrink-0 bg-slate-900 border-r border-slate-800 flex flex-col">
        <div className="h-14 flex items-center gap-2.5 px-5 border-b border-slate-800">
          <Zap className="w-5 h-5 text-blue-400" />
          <span className="text-sm font-semibold tracking-wide text-white">
            Taskboard
          </span>
        </div>

        <nav className="flex-1 py-3 px-2.5 space-y-4">
          <div role="group" aria-labelledby="nav-views">
            <p
              id="nav-views"
              className="px-2.5 pb-1.5 text-[10px] font-medium text-slate-500 tracking-wider uppercase"
            >
              {VIEWS_GROUP_LABEL}
            </p>
            <div className="space-y-0.5">
              {viewItems.map((item) => (
                <NavEntry key={item.to} item={item} search={viewSearch} />
              ))}
            </div>
          </div>
          <div className="space-y-0.5">
            {otherItems.map((item) => (
              <NavEntry key={item.to} item={item} />
            ))}
          </div>
        </nav>

        <div className="px-5 py-3 border-t border-slate-800">
          <p className="text-[10px] text-slate-600 tracking-wider uppercase">
            v0.1.0
          </p>
        </div>
      </aside>

      <div className="flex-1 flex flex-col overflow-hidden">
        <main className="flex-1 overflow-auto bg-slate-950">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
