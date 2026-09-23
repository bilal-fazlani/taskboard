// Sorting the Table by its Epic column.
//
// A click on the column's header steps through ascending, descending and back
// to the table's own order. Epics sort by name, ignoring case; tickets without
// an epic go last either way, as blanks do in a spreadsheet, and tickets in
// the same epic keep the order they had.

export type EpicSort = "asc" | "desc" | null;

/** The sort a click on the header moves to from the given one. */
export function nextEpicSort(sort: EpicSort): EpicSort {
  if (sort === null) return "asc";
  if (sort === "asc") return "desc";
  return null;
}

/** The tickets in the given order, as a new array; the same order without a sort. */
export function sortByEpic<T extends { epic?: { name: string } | null }>(tickets: readonly T[], sort: EpicSort): T[] {
  if (sort === null) return [...tickets];
  const sign = sort === "asc" ? 1 : -1;
  return [...tickets].sort((a, b) => {
    if (!a.epic || !b.epic) return (a.epic ? 0 : 1) - (b.epic ? 0 : 1);
    return sign * a.epic.name.localeCompare(b.epic.name, undefined, { sensitivity: "base" });
  });
}
