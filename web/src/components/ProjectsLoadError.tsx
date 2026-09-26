/**
 * Shown by a view in place of its content when the projects list — which
 * FilterPanel loads and hands up through its `onProjects` prop, the one
 * place every view and the filter bar get it from — has never loaded. A view
 * without a project in its URL can't pick one without that list, so without
 * this it would either hang on its loading message forever or, worse, look
 * like an empty project with nothing in it.
 *
 * The next live refresh (FilterPanel's own useLiveRefresh) retries the load
 * on its own; this is only the explanation while it hasn't yet, in the same
 * two-line, centred style the views already use for their empty states, with
 * the red, role="alert" treatment ProjectTextFields.tsx uses for a load that
 * failed.
 */
export default function ProjectsLoadError() {
  return (
    <div className="flex flex-col items-center justify-center gap-1 h-full text-center">
      <p role="alert" className="text-sm text-red-400">
        Couldn't load the projects
      </p>
      <p className="text-xs text-slate-600">The next refresh will try again.</p>
    </div>
  );
}
