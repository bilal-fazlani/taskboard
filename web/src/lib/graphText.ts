// Text shown on the graph page: column headings and the dependency lines on a
// graph card. Kept apart from the components so the wording and pluralisation
// are unit tested.

/** "Ready" for column 0, then "Blocked · 1 step", "Blocked · 2 steps", ... */
export function columnHeading(index: number): string {
  if (index <= 0) return "Ready";
  return `Blocked · ${index} ${index === 1 ? "step" : "steps"}`;
}

/**
 * The done block's count beside its "Done" header: "50 of 96" while it shows
 * fewer than there are, and just the total, "42", once it shows them all.
 */
export function doneCountText(shown: number, total: number): string {
  return shown < total ? `${shown} of ${total}` : `${total}`;
}

/**
 * "1 of 2 dependencies done", "3 of 3 dependencies done", "1 of 1 dependency
 * done", or null when none are done. The noun agrees with the total, not the
 * done count, since it always names every dependency, not just the done ones.
 */
export function satisfiedDependenciesText(done: number, total: number): string | null {
  if (done <= 0) return null;
  return `${done} of ${total} ${total === 1 ? "dependency" : "dependencies"} done`;
}

/**
 * "1 hidden blocker", "2 hidden blockers", or null when there are none. A
 * hidden blocker is an unfinished dependency that isn't on the page, such as
 * a ticket in a project the selector filters out, so it has no arrow.
 */
export function hiddenBlockersText(count: number): string | null {
  if (count <= 0) return null;
  return `${count} hidden ${count === 1 ? "blocker" : "blockers"}`;
}
