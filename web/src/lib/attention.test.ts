import { describe, expect, it } from "vitest";
import { ATTENTION_CARD_CLASS, ATTENTION_DOT_CLASS, attentionClasses } from "./attention";
import { STATUSES } from "./status";

describe("attentionClasses", () => {
  it("rings and pulses an in-progress card on the graph", () => {
    expect(attentionClasses("in_progress", true)).toEqual({
      card: ATTENTION_CARD_CLASS,
      dot: ATTENTION_DOT_CLASS,
    });
  });

  it("leaves the same ticket alone off the graph, so Kanban and Table stay still", () => {
    expect(attentionClasses("in_progress", false)).toEqual({ card: "", dot: "" });
  });

  it("animates no other status", () => {
    for (const status of STATUSES.filter((s) => s !== "in_progress")) {
      expect(attentionClasses(status, true)).toEqual({ card: "", dot: "" });
    }
  });

  it("animates nothing for a status it does not know", () => {
    expect(attentionClasses("needs_input", true)).toEqual({ card: "", dot: "" });
    expect(attentionClasses("", true)).toEqual({ card: "", dot: "" });
  });
});
