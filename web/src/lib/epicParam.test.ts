import { describe, expect, it } from "vitest";
import { findEpic, withEpic, withEpicName, withoutEpic } from "./epicParam";

const epics = [
  { id: "e1", name: "M4: Documents" },
  { id: "e2", name: "Launch" },
];

describe("epic parameter", () => {
  it("finds an epic by id or by name ignoring case", () => {
    expect(findEpic(epics, "e2")?.name).toBe("Launch");
    expect(findEpic(epics, " m4: documents ")?.id).toBe("e1");
    expect(findEpic(epics, "Nope")).toBeUndefined();
    expect(findEpic(epics, "")).toBeUndefined();
    expect(findEpic(epics, "  ")).toBeUndefined();
  });

  it("sets the epic and drops any open document, and removes both", () => {
    const next = withEpic(new URLSearchParams("project=ACP&doc=Plan.md"), epics[0]);
    expect(next.get("epic")).toBe("M4: Documents");
    expect(next.has("doc")).toBe(false);
    expect(next.get("project")).toBe("ACP");
    expect(withoutEpic(new URLSearchParams("project=ACP&epic=Launch&doc=x.md")).toString()).toBe("project=ACP");
  });

  it("renames the epic in place, keeping its open document", () => {
    const next = withEpicName(new URLSearchParams("project=ACP&epic=Launch&doc=Plan.md"), { name: "Go live" });
    expect(next.get("epic")).toBe("Go live");
    expect(next.get("doc")).toBe("Plan.md");
  });
});
