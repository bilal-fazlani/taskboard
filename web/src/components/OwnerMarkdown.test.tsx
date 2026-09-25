// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";
import OwnerMarkdown from "./OwnerMarkdown";

const at = "2026-09-25T09:00:00Z";
const login: DocumentMeta = {
  id: "img1", name: "Login screen", format: "png", size: 10, width: 40, height: 30, revision: 3, createdAt: at, updatedAt: at,
};
const photo: DocumentMeta = { id: "img2", name: "Photo", format: "jpeg", size: 10, revision: 1, createdAt: at, updatedAt: at };
const docs = [login, photo];
const LOGIN_SRC = "/api/documents/img1/image?rev=3";

afterEach(cleanup);

function images(container: HTMLElement) {
  return Array.from(container.querySelectorAll("img"));
}

describe("OwnerMarkdown images", () => {
  it.each([
    ["plain, with spaces", "![Login](Login screen.png)"],
    ["angle-bracketed", "![Login](<Login screen.png>)"],
    ["percent-encoded", "![Login](Login%20screen.png)"],
    ["in other capitals", "![Login](LOGIN SCREEN.PNG)"],
    ["with a title", '![Login](<Login screen.png> "The login")'],
  ])("shows the owner's image for a reference %s", (_, md) => {
    const { container } = render(<OwnerMarkdown documents={docs}>{md}</OwnerMarkdown>);
    const [img] = images(container);
    expect(img.getAttribute("src")).toBe(LOGIN_SRC);
    expect(img.getAttribute("alt")).toBe("Login");
    expect(img.getAttribute("width")).toBe("40");
    expect(screen.queryByTestId("missing-image")).toBeNull();
  });

  it("answers to .jpeg for a JPEG shown as .jpg", () => {
    const { container } = render(<OwnerMarkdown documents={docs}>{"![](Photo.jpeg) ![](photo.jpg)"}</OwnerMarkdown>);
    expect(images(container).map((i) => i.getAttribute("src"))).toEqual([
      "/api/documents/img2/image?rev=1",
      "/api/documents/img2/image?rev=1",
    ]);
  });

  it("finds spaced references among other text, and several in a paragraph", () => {
    const { container } = render(
      <OwnerMarkdown documents={docs}>{"Before ![a](Login screen.png) between ![b](Photo.jpg) after."}</OwnerMarkdown>,
    );
    expect(images(container).map((i) => i.getAttribute("alt"))).toEqual(["a", "b"]);
    expect(container.textContent).toContain("Before");
    expect(container.textContent).toContain("between");
    expect(container.textContent).toContain("after.");
  });

  it("leaves references in code alone", () => {
    const { container } = render(
      <OwnerMarkdown documents={docs}>{"`![x](Login screen.png)`\n\n```\n![y](Login screen.png)\n```"}</OwnerMarkdown>,
    );
    expect(images(container)).toHaveLength(0);
    expect(container.textContent).toContain("![x](Login screen.png)");
    expect(container.textContent).toContain("![y](Login screen.png)");
  });

  it("keeps absolute URLs as they are", () => {
    const { container } = render(<OwnerMarkdown documents={docs}>{"![logo](https://example.com/logo.png)"}</OwnerMarkdown>);
    expect(images(container)[0].getAttribute("src")).toBe("https://example.com/logo.png");
  });

  it("holds the place quietly while the documents load", () => {
    const { container } = render(<OwnerMarkdown documents={null}>{"![Login](Login screen.png)"}</OwnerMarkdown>);
    expect(images(container)).toHaveLength(0);
    expect(screen.queryByTestId("missing-image")).toBeNull();
    expect(screen.getByTestId("image-loading").textContent).toBe("Login");
  });

  it("keeps the image mounted when the documents reload", () => {
    const { container, rerender } = render(<OwnerMarkdown documents={docs}>{"![Login](Login screen.png)"}</OwnerMarkdown>);
    const before = images(container)[0];
    rerender(<OwnerMarkdown documents={[...docs]}>{"![Login](Login screen.png)"}</OwnerMarkdown>);
    expect(images(container)[0]).toBe(before);
  });
});

describe("OwnerMarkdown missing images", () => {
  it.each([
    ["an unknown name", "![x](Nope.png)", "Nope.png"],
    ["an unknown spaced name", "![x](Not here.png)", "Not here.png"],
    ["another owner's image", "![x](Other ticket shot.png)", "Other ticket shot.png"],
    ["a name without its extension", "![x](<Login screen>)", "Login screen"],
    ["a document that is not an image", "![x](Plan.md)", "Plan.md"],
    ["a path on this server", "![x](/api/documents/img9/image)", "/api/documents/img9/image"],
  ])("marks %s as missing, with no broken image", (_, md, name) => {
    const { container } = render(<OwnerMarkdown documents={docs}>{md}</OwnerMarkdown>);
    expect(images(container)).toHaveLength(0);
    const marker = screen.getByTestId("missing-image");
    expect(marker.getAttribute("role")).toBe("img");
    expect(marker.getAttribute("aria-label")).toBe(`Missing image: ${name}`);
    expect(marker.textContent).toBe(`Missing image: ${name}`);
  });

  it("names the owner in the marker's tooltip", () => {
    render(<OwnerMarkdown documents={docs} ownerNoun="epic">{"![](Nope.png)"}</OwnerMarkdown>);
    expect(screen.getByTestId("missing-image").getAttribute("title")).toBe('This epic has no image called "Nope.png".');
  });

  it("marks a URL the sanitizer drops as missing", () => {
    const { container } = render(<OwnerMarkdown documents={docs}>{"![alt text](javascript:alert(1))"}</OwnerMarkdown>);
    expect(images(container)).toHaveLength(0);
    expect(screen.getByTestId("missing-image").textContent).toBe("Missing image: alt text");
  });

  it("turns an owner image that fails to load into the marker", () => {
    const { container } = render(<OwnerMarkdown documents={docs}>{"![Login](Login screen.png)"}</OwnerMarkdown>);
    fireEvent.error(images(container)[0]);
    expect(images(container)).toHaveLength(0);
    expect(screen.getByTestId("missing-image")).toBeTruthy();
  });
});
