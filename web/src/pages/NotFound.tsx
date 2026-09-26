import { Link, useLocation } from "react-router-dom";

// Shown inside the layout for any path the app has no page for, including
// the removed /board, /tickets and /graph, which have no redirects.
export default function NotFound() {
  const { pathname } = useLocation();
  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-xl font-semibold text-white mb-6">Page not found</h1>
      <p className="text-sm text-slate-400 mb-4">
        There is no page at <code className="text-slate-300">{pathname}</code>.
      </p>
      <Link to="/" className="text-sm text-blue-400 hover:text-blue-300">
        Go to Dependencies
      </Link>
    </div>
  );
}
