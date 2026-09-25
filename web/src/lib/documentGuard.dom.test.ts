// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";

// The history guard the server puts at the top of every HTML document it
// serves raw (internal/server/documentguard.js). It is plain browser
// JavaScript with no build step; this runs it in jsdom to pin what it does
// to the page. jsdom has no SVG links and no Navigation API, so SVG links
// are checked in a real browser and Navigation is a stand-in here.

const calls: { url: string; options: unknown }[] = [];
let takenOver = false;

beforeAll(() => {
  class Navigation {
    navigate(url: string, options?: unknown) {
      calls.push({ url, options });
    }
  }
  (globalThis as { Navigation?: unknown }).Navigation = Navigation;
  // A path, not a URL: under jsdom the global URL is jsdom's, which fs refuses.
  const here = dirname(fileURLToPath(import.meta.url));
  const source = readFileSync(resolve(here, "../../../internal/server/documentguard.js"), "utf8");
  new Function(source)();
  // Registered after the guard, so it sees what the guard decided; it then
  // stops jsdom's own navigation, so a link left to the browser goes nowhere.
  window.addEventListener("click", (event) => {
    takenOver = event.defaultPrevented;
    event.preventDefault();
  });
});

beforeEach(() => {
  window.history.replaceState(null, "", "/api/documents/x/raw");
  document.body.innerHTML = `
    <a id="toc" href="#s2">Two</a>
    <a id="other" href="/api/documents/y/raw">Other</a>
    <a id="blank" href="#s2" target="_blank">New window</a>
    <a id="self" href="#s2" target="_self">Self</a>
    <a id="download" href="#s2" download>Save</a>
    <a id="mail" href="mailto:someone@example.com">Mail</a>
    <a id="plain">No href</a>
    <a id="wrap" href="#s3"><span id="inner">Three</span></a>
    <map name="m"><area id="area" href="#s2" shape="rect" coords="0,0,1,1"></map>
    <h2 id="s2">Two</h2><h2 id="s3">Three</h2>`;
});

// Whether the guard took the click over from the browser.
function click(id: string, init: MouseEventInit = {}): boolean {
  takenOver = false;
  document.getElementById(id)!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, ...init }));
  return takenOver;
}

describe("the document history guard", () => {
  it("turns pushState into replaceState", () => {
    const length = window.history.length;
    window.history.pushState({ page: 2 }, "", "#pushed");
    expect(window.history.length).toBe(length);
    expect(window.location.hash).toBe("#pushed");
    expect(window.history.state).toEqual({ page: 2 });
  });

  it("follows an in-page link without adding an entry", () => {
    const length = window.history.length;
    expect(click("toc")).toBe(true);
    expect(window.location.hash).toBe("#s2");
    expect(window.history.length).toBe(length);
  });

  it("finds the link a click inside it belongs to, and area links", () => {
    expect(click("inner")).toBe(true);
    expect(window.location.hash).toBe("#s3");
    expect(click("area")).toBe(true);
    expect(window.location.hash).toBe("#s2");
  });

  it("takes over links to other pages and target=_self", () => {
    expect(click("self")).toBe(true);
    // jsdom cannot leave the page; the real browser check covers where it goes.
    expect(click("other")).toBe(true);
  });

  it("leaves links with a target, a download or a modifier key to the browser", () => {
    expect(click("blank")).toBe(false);
    expect(click("download")).toBe(false);
    for (const key of ["metaKey", "ctrlKey", "shiftKey", "altKey"] as const) {
      expect(click("toc", { [key]: true })).toBe(false);
    }
    expect(click("toc", { button: 1 })).toBe(false);
  });

  it("leaves links that are not http(s), and elements that are not links, alone", () => {
    expect(click("mail")).toBe(false);
    expect(click("plain")).toBe(false);
    expect(click("s2")).toBe(false);
  });

  it("does nothing when the page already handled the click", () => {
    const link = document.getElementById("toc")!;
    link.addEventListener("click", (event) => event.preventDefault());
    click("toc");
    expect(window.location.hash).toBe("");
  });

  it("makes navigation.navigate replace", () => {
    const Navigation = (globalThis as unknown as { Navigation: new () => { navigate(u: string, o?: unknown): void } })
      .Navigation;
    calls.length = 0;
    new Navigation().navigate("#a", { history: "push", state: 1 });
    new Navigation().navigate("#b");
    expect(calls).toEqual([
      { url: "#a", options: { history: "replace", state: 1 } },
      { url: "#b", options: { history: "replace" } },
    ]);
  });
});
