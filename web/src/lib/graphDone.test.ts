import { describe, expect, it } from "vitest";
import { DONE_BLOCK_LIMIT, recentDone, type DoneTicket } from "./graphDone";

function done(key: string, doneAt?: string, extra: Partial<DoneTicket> = {}): DoneTicket {
  const [prefix, number] = key.split("-");
  return { id: key, projectPrefix: prefix, number: Number(number), status: "done", doneAt, ...extra };
}

const ids = (tickets: readonly DoneTicket[]) => tickets.map((t) => t.id);

describe("recentDone", () => {
  it("holds 50 at most", () => {
    expect(DONE_BLOCK_LIMIT).toBe(50);
  });

  it("orders done tickets by when they moved to done, newest first, and leaves open ones out", () => {
    const picked = recentDone([
      done("A-1", "2026-09-20T10:00:00Z"),
      { ...done("A-2"), status: "todo" },
      done("A-3", "2026-09-22T10:00:00Z"),
      done("A-4", "2026-09-21T10:00:00.5Z"),
      { ...done("A-5"), status: "in_progress" },
    ]);
    expect(ids(picked.tickets)).toEqual(["A-3", "A-4", "A-1"]);
    expect(picked.total).toBe(3);
  });

  it("compares the times, not their text, whatever their fraction digits", () => {
    // As text, ".5Z" sorts before "Z"; as times, the half second is later.
    const picked = recentDone([done("A-1", "2026-09-21T10:00:00Z"), done("A-2", "2026-09-21T10:00:00.5Z")]);
    expect(ids(picked.tickets)).toEqual(["A-2", "A-1"]);
  });

  it("never reads updatedAt: an edit to a done ticket doesn't move it", () => {
    const edited = { ...done("A-1", "2026-09-20T10:00:00Z"), updatedAt: "2026-09-26T10:00:00Z" };
    const picked = recentDone([edited, done("A-2", "2026-09-21T10:00:00Z")]);
    expect(ids(picked.tickets)).toEqual(["A-2", "A-1"]);
  });

  it("takes createdAt for a ticket without doneAt, and one with neither last", () => {
    const picked = recentDone([
      done("A-1", undefined, { createdAt: "2026-09-10T10:00:00Z" }),
      done("A-2"),
      done("A-3", "2026-09-20T10:00:00Z"),
      done("A-4", undefined, { createdAt: "2026-09-12T10:00:00Z" }),
    ]);
    expect(ids(picked.tickets)).toEqual(["A-3", "A-4", "A-1", "A-2"]);
  });

  it("keeps the newest `limit`, counting every done ticket", () => {
    const tickets = Array.from({ length: 60 }, (_, i) =>
      done(`A-${i + 1}`, new Date(Date.UTC(2026, 8, 1) + i * 60_000).toISOString()),
    );
    const picked = recentDone(tickets);
    expect(picked.tickets).toHaveLength(50);
    expect(picked.total).toBe(60);
    expect(picked.tickets[0].id).toBe("A-60");
    expect(picked.tickets[49].id).toBe("A-11");
    expect(recentDone(tickets, 3).tickets.map((t) => t.id)).toEqual(["A-60", "A-59", "A-58"]);
  });

  it("puts a newly done ticket at the top and drops the oldest past the limit", () => {
    const tickets = Array.from({ length: 50 }, (_, i) =>
      done(`A-${i + 1}`, new Date(Date.UTC(2026, 8, 1) + i * 60_000).toISOString()),
    );
    const before = recentDone(tickets);
    expect(before.tickets[49].id).toBe("A-1");
    const after = recentDone([...tickets, done("A-99", "2026-09-26T00:00:00Z")]);
    expect(after.tickets[0].id).toBe("A-99");
    expect(after.tickets).toHaveLength(50);
    expect(ids(after.tickets)).not.toContain("A-1");
    expect(after.total).toBe(51);
  });

  it("breaks a tie by the later ticket first, whatever the input order", () => {
    const at = "2026-09-20T10:00:00Z";
    const tickets = [done("A-2", at), done("B-1", at), done("A-10", at)];
    expect(ids(recentDone(tickets).tickets)).toEqual(["B-1", "A-10", "A-2"]);
    expect(ids(recentDone([...tickets].reverse()).tickets)).toEqual(["B-1", "A-10", "A-2"]);
  });
});
