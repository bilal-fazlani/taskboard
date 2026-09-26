import { describe, expect, it } from "vitest";
import { actionErrorMessage, DELETE_GONE_MESSAGE, deleteErrorMessage, GONE_MESSAGE, saveErrorMessage } from "./saveError";

describe("saveErrorMessage", () => {
  it("says the ticket is gone for a 404, since that is what a deleted ticket looks like", () => {
    expect(saveErrorMessage(new Error('API error 404: {"error":"ticket not found"}'))).toBe(GONE_MESSAGE);
    expect(GONE_MESSAGE).toContain("Your edits are still here");
  });

  it("passes on what the API refused the save for", () => {
    expect(saveErrorMessage(new Error('API error 400: {"error":"due date must be YYYY-MM-DD"}'))).toBe(
      "due date must be YYYY-MM-DD. Your edits are still here.",
    );
  });

  it("does not double the full stop the API already wrote", () => {
    expect(saveErrorMessage(new Error('API error 400: {"error":"no."}'))).toBe("no. Your edits are still here.");
  });

  it("falls back to a body that is not the API's own shape", () => {
    expect(saveErrorMessage(new Error("API error 500: upstream exploded"))).toBe(
      "upstream exploded. Your edits are still here.",
    );
  });

  it("says something useful when there is no message at all", () => {
    const generic = saveErrorMessage(new Error("API error 500: "));
    expect(generic).toContain("Your edits are still here");
    expect(saveErrorMessage(new Error("API error 503: {}"))).toBe(generic);
    // A fetch that never reached the server throws the browser's own wording,
    // which tells a user nothing.
    expect(saveErrorMessage(new TypeError("Failed to fetch"))).toBe(generic);
    expect(saveErrorMessage(undefined)).toBe(generic);
  });
});

describe("deleteErrorMessage", () => {
  it("says the ticket was already deleted for a 404, with no edits framing", () => {
    expect(deleteErrorMessage(new Error('API error 404: {"error":"ticket not found"}'))).toBe(DELETE_GONE_MESSAGE);
    expect(DELETE_GONE_MESSAGE).not.toContain("edits");
  });

  it("passes on what the API refused the delete for", () => {
    expect(deleteErrorMessage(new Error('API error 400: {"error":"ticket has open dependents"}'))).toBe(
      "ticket has open dependents.",
    );
  });

  it("falls back to a generic line with no edits framing", () => {
    const generic = deleteErrorMessage(new Error("API error 500: "));
    expect(generic).not.toContain("edits");
    expect(deleteErrorMessage(new TypeError("Failed to fetch"))).toBe(generic);
    expect(deleteErrorMessage(undefined)).toBe(generic);
  });
});

describe("actionErrorMessage", () => {
  it("passes on what the API refused the action for", () => {
    expect(actionErrorMessage(new Error('API error 400: {"error":"subtask not found"}'))).toBe(
      "subtask not found.",
    );
  });

  it("falls back to a generic line for anything else", () => {
    const generic = actionErrorMessage(new Error("API error 500: "));
    expect(actionErrorMessage(new TypeError("Failed to fetch"))).toBe(generic);
    expect(actionErrorMessage(undefined)).toBe(generic);
  });
});
