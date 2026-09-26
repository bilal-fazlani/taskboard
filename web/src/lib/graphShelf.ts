// Shelves on the dependency graph: blocks of cards with no arrows, laid out
// in balanced columns beside the linked graph. The Ready tickets nothing
// links and no agent holds fill one, left of the linked Ready column. Pure
// functions over plain numbers, with no React and no DOM.
//
// The balancing rule, balanceShelf: a shelf is as tall as the linked graph
// beside it, but never flatter than a square. The rows a column holds are
// the larger of the rows that fit the graph's height and the rows that make
// the shelf square in pixels; the column count follows from that, and the
// cards are split so column lengths differ by at most one, the longer
// columns first. Cards fill each column top to bottom before the next one,
// left to right, and placeShelf stacks each column's own measured cards
// rowGap apart from the top, so the rule only decides how many cards each
// column holds.

export interface ShelfSize {
  width: number;
  height: number;
}

export interface ShelfGaps {
  /** Horizontal space between two columns of the shelf. */
  columnGap: number;
  /** Vertical space between two cards in a column. */
  rowGap: number;
}

/**
 * How many cards each column of a shelf holds, left to right, for cards of
 * the given measured sizes, in shelf order, beside a graph `height` tall.
 * Empty for no cards.
 *
 * A card is taken as the widest card wide, which is what a column is, and as
 * the cards' mean height tall. Rows that fit is the most cards of that height
 * stacked rowGap apart within `height`, at least 1, so a height of 0 leaves
 * the square rule to decide. Square rows is
 * ceil(sqrt(n × (width + columnGap) / (height + rowGap))).
 */
export function balanceShelf(sizes: readonly ShelfSize[], height: number, gaps: ShelfGaps): number[] {
  const n = sizes.length;
  if (n === 0) return [];
  let width = 0;
  let total = 0;
  for (const size of sizes) {
    width = Math.max(width, size.width);
    total += size.height;
  }
  const card = total / n;
  const pitch = card + gaps.rowGap;
  const fit = pitch > 0 ? Math.max(1, Math.floor((height + gaps.rowGap) / pitch)) : n;
  const square = pitch > 0 ? Math.ceil(Math.sqrt((n * (width + gaps.columnGap)) / pitch)) : n;
  const rows = Math.max(fit, square);
  return splitEvenly(n, Math.ceil(n / rows));
}

/**
 * `n` split into `columns` lengths that differ by at most one, the longer
 * ones first: 34 over 3 is 12, 11, 11.
 */
export function splitEvenly(n: number, columns: number): number[] {
  if (n <= 0 || columns <= 0) return [];
  const base = Math.floor(n / columns);
  const longer = n % columns;
  return Array.from({ length: columns }, (_, i) => base + (i < longer ? 1 : 0));
}

export interface PlacedShelf {
  /** Top-left corner of each card, in the order the sizes came in. */
  cards: { x: number; y: number }[];
  /** Each column's left edge, width (its widest card) and card count. */
  columns: { x: number; width: number; count: number }[];
  width: number;
  /** From the top to the bottom of the tallest column. */
  height: number;
}

/**
 * Places a shelf's cards from `origin`: column by column, `lengths` cards
 * each (from balanceShelf), top to bottom rowGap apart, each column as wide
 * as its widest card and columnGap after it.
 */
export function placeShelf(
  sizes: readonly ShelfSize[],
  lengths: readonly number[],
  origin: { x: number; y: number },
  gaps: ShelfGaps,
): PlacedShelf {
  const cards: { x: number; y: number }[] = [];
  const columns: PlacedShelf["columns"] = [];
  let x = origin.x;
  let height = 0;
  let next = 0;
  for (const count of lengths) {
    let width = 0;
    let y = origin.y;
    for (let i = 0; i < count; i++) {
      const size = sizes[next + i];
      width = Math.max(width, size.width);
      cards.push({ x, y });
      y += size.height + gaps.rowGap;
    }
    next += count;
    height = Math.max(height, y - gaps.rowGap - origin.y);
    columns.push({ x, width, count });
    x += width + gaps.columnGap;
  }
  const last = columns[columns.length - 1];
  return { cards, columns, width: last ? last.x + last.width - origin.x : 0, height };
}
