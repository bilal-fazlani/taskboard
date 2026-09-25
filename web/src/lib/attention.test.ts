/// <reference types="node" />
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  ATTENTION_CARD_CLASS,
  ATTENTION_DOT_CLASS,
  ATTENTION_REVIEW_CARD_CLASS,
  ATTENTION_REVIEW_DOT_CLASS,
  attentionClasses,
} from "./attention";
import { STATUSES } from "./status";

describe("attentionClasses", () => {
  it("rings and pulses an in-progress card on the graph", () => {
    expect(attentionClasses("in_progress", true)).toEqual({
      card: ATTENTION_CARD_CLASS,
      dot: ATTENTION_DOT_CLASS,
    });
  });

  it("rings and pulses an agent_review card on the graph, in the violet pair", () => {
    expect(attentionClasses("agent_review", true)).toEqual({
      card: ATTENTION_REVIEW_CARD_CLASS,
      dot: ATTENTION_REVIEW_DOT_CLASS,
    });
  });

  it("gives the two active statuses different classes, so the colours differ", () => {
    expect(ATTENTION_REVIEW_CARD_CLASS).not.toBe(ATTENTION_CARD_CLASS);
    expect(ATTENTION_REVIEW_DOT_CLASS).not.toBe(ATTENTION_DOT_CLASS);
  });

  it("leaves the same tickets alone off the graph, so Kanban and Table stay still", () => {
    expect(attentionClasses("in_progress", false)).toEqual({ card: "", dot: "" });
    expect(attentionClasses("agent_review", false)).toEqual({ card: "", dot: "" });
  });

  it("animates no other status", () => {
    for (const status of STATUSES.filter((s) => s !== "in_progress" && s !== "agent_review")) {
      expect(attentionClasses(status, true)).toEqual({ card: "", dot: "" });
    }
  });

  it("animates nothing for a status it does not know", () => {
    expect(attentionClasses("needs_input", true)).toEqual({ card: "", dot: "" });
    expect(attentionClasses("", true)).toEqual({ card: "", dot: "" });
  });
});

// The class names above are only half the contract: the animations themselves
// live in src/index.css, and a card wearing a class no stylesheet defines
// would look like a plain card.
describe("the stylesheet behind the classes", () => {
  // Read as a file rather than imported: Vitest stubs a CSS import, including
  // one with ?raw, with an empty string.
  const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");

  for (const className of [
    ATTENTION_CARD_CLASS,
    ATTENTION_DOT_CLASS,
    ATTENTION_REVIEW_CARD_CLASS,
    ATTENTION_REVIEW_DOT_CLASS,
  ]) {
    it(`animates .${className} and holds it still under reduced motion`, () => {
      const rules = [...css.matchAll(new RegExp(`\\.${className}\\s*\\{([^}]*)\\}`, "g"))].map(
        (m) => m[1],
      );
      expect(rules).toHaveLength(2);
      expect(rules[0]).toMatch(/animation:/);
      expect(rules[1]).toMatch(/animation:\s*none/);
      expect(rules[1]).toMatch(/box-shadow:/);
    });
  }

  it("rings agent review in violet where in progress is blue, both derived from the status colour tokens", () => {
    const ring = (name: string) =>
      css.match(new RegExp(`@keyframes ${name}\\s*\\{[\\s\\S]*?\\n\\}`))![0];
    expect(ring("graph-attention-ring")).toContain("var(--color-blue-500)");
    expect(ring("graph-attention-review-ring")).toContain("var(--color-violet-500)");
    expect(ring("graph-attention-review-ring")).not.toContain("var(--color-blue-500)");
    // No hardcoded rgb() standing in for the token: the whole file, not just
    // these two rules, since a stray literal anywhere would drift from
    // lib/status.ts's STATUS_COLORS if the palette ever changed.
    expect(css).not.toMatch(/rgb\(\s*59\s+130\s+246/);
    expect(css).not.toMatch(/rgb\(\s*139\s+92\s+246/);
  });
});
