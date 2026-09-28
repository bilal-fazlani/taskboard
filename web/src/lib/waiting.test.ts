import { describe, expect, it } from "vitest";
import {
  WAITING_LINK,
  compareWaiting,
  firstLine,
  requestKind,
  waitingCount,
  waitingLabel,
  waitingSince,
} from "./waiting";

describe("waitingLabel", () => {
  it("says a waiting ticket waits on you, with its request's type and first line", () => {
    expect(
      waitingLabel({ status: "needs_user_input", openRequest: { type: "approval", prompt: "Land it?\nThe suite is green." } }),
    ).toBe(", waiting on you: Approve: Land it?");
    expect(waitingLabel({ status: "needs_user_input" })).toBe(", waiting on you");
  });

  it("adds nothing for a ticket that isn't waiting", () => {
    expect(waitingLabel({ status: "in_progress" })).toBe("");
  });
});

describe("requestKind", () => {
  it("names an approval Approve and a question Question", () => {
    expect(requestKind("approval")).toBe("Approve");
    expect(requestKind("question")).toBe("Question");
  });

  it("names a later type by its own name, so a newer server's type still reads", () => {
    expect(requestKind("choice")).toBe("Choice");
    expect(requestKind("pick_one")).toBe("Pick one");
    expect(requestKind("")).toBe("Request");
  });
});

describe("firstLine", () => {
  it("is the prompt's first line that says something, trimmed", () => {
    expect(firstLine("Land it?\nThe suite is green.")).toBe("Land it?");
    expect(firstLine("\n  \r\n  Which port?  \r\nMore")).toBe("Which port?");
    expect(firstLine("")).toBe("");
  });
});

describe("waitingSince and compareWaiting", () => {
  const asked = (at: string) => ({ updatedAt: "2026-09-28T12:00:00Z", openRequest: { createdAt: at } });

  it("reads when the request was made, or the last update without one", () => {
    expect(waitingSince(asked("2026-09-28T10:00:00Z"))).toBe("2026-09-28T10:00:00Z");
    expect(waitingSince({ updatedAt: "2026-09-28T09:00:00Z" })).toBe("2026-09-28T09:00:00Z");
    expect(waitingSince({})).toBe("");
  });

  it("sorts oldest first, with an unreadable time last", () => {
    const list = [asked("2026-09-28T11:00:00Z"), {}, asked("2026-09-28T09:00:00Z"), asked("2026-09-28T10:00:00Z")];
    expect([...list].sort(compareWaiting).map(waitingSince)).toEqual([
      "2026-09-28T09:00:00Z",
      "2026-09-28T10:00:00Z",
      "2026-09-28T11:00:00Z",
      "",
    ]);
  });
});

describe("waitingCount", () => {
  it("counts only the tickets waiting on the person", () => {
    expect(
      waitingCount([
        { status: "needs_user_input" },
        { status: "in_progress" },
        { status: "needs_user_input" },
        { status: "agent_review" },
      ]),
    ).toBe(2);
    expect(waitingCount([])).toBe(0);
  });
});

describe("WAITING_LINK", () => {
  it("opens Now's waiting group across every project", () => {
    expect(WAITING_LINK).toBe("/now?project=all#waiting");
  });
});
