import { describe, expect, it } from "vitest";
import type { TicketRequest } from "../api/client";
import { answerToast, answerTone, askedAgo, collectorPhrase, earlierRequests, questionAnswer } from "./requests";

function req(overrides: Partial<TicketRequest>): TicketRequest {
  return {
    id: "r1",
    ticketId: "t1",
    agentId: "a1",
    type: "question",
    prompt: "Which?",
    choices: [],
    createdAt: "2026-09-28T09:00:00Z",
    ...overrides,
  };
}

describe("questionAnswer", () => {
  it("sends the typed text over a picked choice", () => {
    expect(questionAnswer("Round only", "  Neither: drop the marks  ")).toBe("Neither: drop the marks");
  });
  it("sends the choice when nothing is typed, or only whitespace", () => {
    expect(questionAnswer("Round only", "   ")).toBe("Round only");
  });
  it("is empty with neither, so the form can't be sent", () => {
    expect(questionAnswer("", "")).toBe("");
  });
});

describe("earlierRequests", () => {
  it("leaves out the open request and puts the newest first", () => {
    const all = [
      req({ id: "old", createdAt: "2026-09-27T09:00:00Z" }),
      req({ id: "open", createdAt: "2026-09-28T10:00:00Z" }),
      req({ id: "mid", createdAt: "2026-09-28T08:00:00Z" }),
    ];
    expect(earlierRequests(all, "open").map((r) => r.id)).toEqual(["mid", "old"]);
    expect(earlierRequests(all, undefined).map((r) => r.id)).toEqual(["open", "mid", "old"]);
  });
});

describe("askedAgo", () => {
  const now = Date.parse("2026-09-28T12:00:00Z");
  it("words the age in the largest whole unit", () => {
    expect(askedAgo("2026-09-28T11:59:30Z", now)).toBe("just now");
    expect(askedAgo("2026-09-28T11:57:00Z", now)).toBe("3m ago");
    expect(askedAgo("2026-09-28T09:10:00Z", now)).toBe("2h ago");
    expect(askedAgo("2026-09-24T12:00:00Z", now)).toBe("4d ago");
  });
  it("says nothing for an unreadable time", () => {
    expect(askedAgo("", now)).toBe("");
  });
});

describe("toast and labels", () => {
  const agent = { role: "implementer", model: "gpt-5" };
  it("names the agent that collects the answer", () => {
    expect(answerToast("question", "Round only", agent)).toBe("Answer sent. The implementer (gpt-5) gets it on its next call.");
    expect(answerToast("approval", "approved", agent)).toBe("Approved. The implementer (gpt-5) gets it on its next call.");
    expect(answerToast("approval", "declined", undefined)).toBe("Declined. The agent that asked gets it on its next call.");
    expect(collectorPhrase({ role: "reviewer", model: "" })).toBe("The reviewer");
  });
  it("colours an answer by what it says", () => {
    expect(answerTone(req({ type: "approval", answer: "declined", answeredAt: "x" }))).toBe("declined");
    expect(answerTone(req({ type: "approval", answer: "approved", answeredAt: "x" }))).toBe("approved");
    expect(answerTone(req({ answer: "Round only", answeredAt: "x" }))).toBe("answer");
    expect(answerTone(req({}))).toBe("waiting");
  });
  it("keeps a request closed by stopping work neutral, whatever its type, going by `stopped` and not the answer text", () => {
    // The server's models.StoppedAnswer, as it arrives.
    const stopped = "Not answered: the person stopped work on this ticket.";
    expect(answerTone(req({ type: "approval", answer: stopped, answeredAt: "x", stopped: true }))).toBe("closed");
    expect(answerTone(req({ type: "question", answer: stopped, answeredAt: "x", stopped: true }))).toBe("closed");
  });
  it("pins that answerTone branches on `stopped`, not on matching the answer's text, even though the real server never sends one without the other", () => {
    // The server always sets `stopped: true` alongside this exact text
    // (models.StoppedAnswer), so this combination never arrives for real;
    // it only isolates which of the two fields answerTone actually reads.
    const looksStopped = "Not answered: the person stopped work on this ticket.";
    expect(answerTone(req({ type: "question", answer: looksStopped, answeredAt: "x" }))).toBe("answer");
  });
  it("calls an approval approved or declined only for exactly those answers", () => {
    expect(answerTone(req({ type: "approval", answer: "yes", answeredAt: "x" }))).toBe("closed");
    expect(answerToast("approval", "Not answered: the person stopped work on this ticket.", undefined)).toBe(
      "Answer sent. The agent that asked gets it on its next call.",
    );
  });
});
