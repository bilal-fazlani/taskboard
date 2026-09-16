// The query string as it is right now, for code that changes one parameter
// and must keep the rest.
//
// React Router 7 renders each navigation inside startTransition, so the
// params a component rendered with (and the ones its functional
// setSearchParams hands over) can lag behind the URL: two changes in quick
// succession, say a keystroke and then a select, would each start from the
// same stale params and the second would undo the first. BrowserRouter writes
// the new URL to window.history synchronously, so window.location is always
// the latest.
//
// This assumes the app runs under BrowserRouter, as it does. Under another
// router in a browser (a MemoryRouter in a DOM test, say) window.location is
// not the router's URL and this would read the wrong one; the only fallback is
// for when there is no window at all (server rendering, Node tests), where the
// router's own params are used.
export function latestSearchParams(fallback: URLSearchParams): URLSearchParams {
  if (typeof window === "undefined") return new URLSearchParams(fallback);
  return new URLSearchParams(window.location.search);
}
