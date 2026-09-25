// The open epic modal, held in the Epics view's URL as `epic=<name>`, with
// its open document as `doc=`. Epic names are unique within a project, and
// the view shows one project, so the name is enough and reads better than
// an id; an id is accepted too.
import { DOC_PARAM } from "./documents";

export const EPIC_PARAM = "epic";

/** The epic an `epic` value names: its id, or its name ignoring case. */
export function findEpic<T extends { id: string; name: string }>(epics: readonly T[], ref: string): T | undefined {
  const wanted = ref.trim();
  if (!wanted) return undefined;
  const lower = wanted.toLowerCase();
  return epics.find((e) => e.id === wanted) ?? epics.find((e) => e.name.trim().toLowerCase() === lower);
}

/** Open an epic: set it, and drop any document another epic had open. */
export function withEpic(params: URLSearchParams, epic: { name: string }): URLSearchParams {
  const next = withEpicName(params, epic);
  next.delete(DOC_PARAM);
  return next;
}

/** Name the open epic by its current name, keeping its open document. */
export function withEpicName(params: URLSearchParams, epic: { name: string }): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(EPIC_PARAM, epic.name);
  return next;
}

export function withoutEpic(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(EPIC_PARAM);
  next.delete(DOC_PARAM);
  return next;
}
