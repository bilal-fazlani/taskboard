import { describe, expect, it } from "vitest";
import { groupByRepo, hasDelivery, prLabel } from "./delivery";

describe("hasDelivery", () => {
  it("is true only when some field is set", () => {
    expect(hasDelivery(undefined)).toBe(false);
    expect(hasDelivery({})).toBe(false);
    expect(hasDelivery({ branch: "", worktree: "", prUrl: "", landedCommits: [] })).toBe(false);
    expect(hasDelivery({ worktree: "/w" })).toBe(true);
    expect(hasDelivery({ landedCommits: [{ sha: "0123abc", repo: "acme/api" }] })).toBe(true);
  });
});

describe("prLabel", () => {
  it("shortens a GitHub pull request url to owner/repo#N", () => {
    expect(prLabel("https://github.com/acme/api/pull/12")).toBe("acme/api#12");
    expect(prLabel("https://github.com/acme/api/pull/12/files")).toBe("acme/api#12");
  });

  it("leaves any other url as it is", () => {
    expect(prLabel("https://github.com/acme/api/issues/12")).toBe("https://github.com/acme/api/issues/12");
    expect(prLabel("https://gitlab.com/acme/api/-/merge_requests/3")).toBe("https://gitlab.com/acme/api/-/merge_requests/3");
  });
});

describe("groupByRepo", () => {
  it("keeps repos in first-seen order and commits in the order given", () => {
    expect(
      groupByRepo([
        { sha: "1111111", repo: "acme/web" },
        { sha: "2222222", repo: "acme/api" },
        { sha: "3333333", repo: "acme/web" },
      ]),
    ).toEqual([
      ["acme/web", [{ sha: "1111111", repo: "acme/web" }, { sha: "3333333", repo: "acme/web" }]],
      ["acme/api", [{ sha: "2222222", repo: "acme/api" }]],
    ]);
  });
});
