import { describe, expect, it } from "vitest";
import { balanceShelf, placeShelf, splitEvenly, type ShelfSize } from "./graphShelf";

const CARD: ShelfSize = { width: 280, height: 96 };
const GAPS = { columnGap: 24, rowGap: 16 };
const cards = (n: number, size: ShelfSize = CARD) => Array.from({ length: n }, () => size);

describe("splitEvenly", () => {
  it("puts the longer columns first, differing by at most one", () => {
    expect(splitEvenly(34, 3)).toEqual([12, 11, 11]);
    expect(splitEvenly(35, 3)).toEqual([12, 12, 11]);
    expect(splitEvenly(36, 3)).toEqual([12, 12, 12]);
    expect(splitEvenly(7, 1)).toEqual([7]);
    expect(splitEvenly(3, 5)).toEqual([1, 1, 1, 0, 0]);
  });

  it("is empty for nothing to split", () => {
    expect(splitEvenly(0, 3)).toEqual([]);
    expect(splitEvenly(4, 0)).toEqual([]);
  });

  it("differs by at most one and sums to n for every split", () => {
    for (let n = 1; n <= 60; n++) {
      for (let columns = 1; columns <= n; columns++) {
        const lengths = splitEvenly(n, columns);
        expect(lengths).toHaveLength(columns);
        expect(lengths.reduce((a, b) => a + b, 0)).toBe(n);
        expect(Math.max(...lengths) - Math.min(...lengths)).toBeLessThanOrEqual(1);
        expect([...lengths].sort((a, b) => b - a)).toEqual(lengths);
      }
    }
  });
});

describe("balanceShelf", () => {
  it("is empty for no cards", () => {
    expect(balanceShelf([], 1000, GAPS)).toEqual([]);
  });

  it("follows the graph's height when that is taller than a square", () => {
    // The mock's numbers: 34 cards beside a graph 1410px tall. 12 cards of 96
    // stacked 16 apart take 1328px and a 13th doesn't fit; a square needs
    // only ceil(sqrt(34 × 304 / 112)) = 10 rows.
    expect(balanceShelf(cards(34), 1410, GAPS)).toEqual([12, 11, 11]);
  });

  it("counts a card that fits the height exactly", () => {
    // 12 × 96 + 11 × 16 = 1328.
    expect(balanceShelf(cards(34), 1328, GAPS)).toEqual([12, 11, 11]);
    // One pixel less and 11 rows fit: 4 columns of 34 split evenly.
    expect(balanceShelf(cards(34), 1327, GAPS)).toEqual([9, 9, 8, 8]);
  });

  it("never goes flatter than a square", () => {
    // A short graph: 2 rows fit, but a square of 34 cards needs 10 rows,
    // so 4 columns, split evenly.
    expect(balanceShelf(cards(34), 208, GAPS)).toEqual([9, 9, 8, 8]);
    // No graph at all: one row fits, the square decides.
    expect(balanceShelf(cards(34), 0, GAPS)).toEqual([9, 9, 8, 8]);
    // ceil(sqrt(4 × 304 / 112)) = 4 rows: one column, taller than wide.
    expect(balanceShelf(cards(4), 0, GAPS)).toEqual([4]);
    // ceil(sqrt(12 × 304 / 112)) = 6 rows: two columns of 6.
    expect(balanceShelf(cards(12), 0, GAPS)).toEqual([6, 6]);
    expect(balanceShelf(cards(1), 0, GAPS)).toEqual([1]);
  });

  it("takes the square's rows when no graph is beside it", () => {
    for (let n = 1; n <= 80; n++) {
      const square = Math.ceil(Math.sqrt((n * (CARD.width + GAPS.columnGap)) / (CARD.height + GAPS.rowGap)));
      const lengths = balanceShelf(cards(n), 0, GAPS);
      expect(lengths).toEqual(splitEvenly(n, Math.ceil(n / square)));
      // That many rows is never flatter than a square in pixels.
      expect(square * square * (CARD.height + GAPS.rowGap)).toBeGreaterThanOrEqual(n * (CARD.width + GAPS.columnGap));
    }
  });

  it("puts a few cards in one column beside a tall graph", () => {
    expect(balanceShelf(cards(3), 2000, GAPS)).toEqual([3]);
  });

  it("uses the widest card and the mean height", () => {
    // Mean height 96 over cards of 64 and 128; the widest is 280.
    const mixed = Array.from({ length: 34 }, (_, i) => ({ width: i === 5 ? 280 : 200, height: i % 2 === 0 ? 64 : 128 }));
    expect(balanceShelf(mixed, 1410, GAPS)).toEqual([12, 11, 11]);
    // Narrower cards square up with fewer rows: ceil(sqrt(34 × 124 / 112)) = 7.
    expect(balanceShelf(cards(34, { width: 100, height: 96 }), 0, GAPS)).toEqual([7, 7, 7, 7, 6]);
  });

  it("takes every card's column in fill order, the longer columns first", () => {
    for (const [n, height] of [
      [5, 300],
      [17, 900],
      [50, 1200],
      [50, 0],
    ] as const) {
      const lengths = balanceShelf(cards(n), height, GAPS);
      expect(lengths.reduce((a, b) => a + b, 0)).toBe(n);
      expect(Math.max(...lengths) - Math.min(...lengths)).toBeLessThanOrEqual(1);
      expect([...lengths].sort((a, b) => b - a)).toEqual(lengths);
    }
  });
});

describe("placeShelf", () => {
  it("fills each column top to bottom, then the next to the right, each card rowGap under the one above", () => {
    const sizes = [
      { width: 256, height: 100 },
      { width: 256, height: 80 },
      { width: 256, height: 120 },
      { width: 240, height: 90 },
      { width: 256, height: 70 },
    ];
    const shelf = placeShelf(sizes, [3, 2], { x: 10, y: 50 }, GAPS);
    expect(shelf.cards).toEqual([
      { x: 10, y: 50 },
      { x: 10, y: 166 },
      { x: 10, y: 262 },
      { x: 290, y: 50 },
      { x: 290, y: 156 },
    ]);
    expect(shelf.columns).toEqual([
      { x: 10, width: 256, count: 3 },
      { x: 290, width: 256, count: 2 },
    ]);
    expect(shelf.width).toBe(290 + 256 - 10);
    // The tallest column: 100 + 16 + 80 + 16 + 120.
    expect(shelf.height).toBe(332);
  });

  it("is empty for no columns", () => {
    expect(placeShelf([], [], { x: 5, y: 5 }, GAPS)).toEqual({ cards: [], columns: [], width: 0, height: 0 });
  });
});
