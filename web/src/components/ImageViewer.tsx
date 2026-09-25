import { useState } from "react";
import { api, type DocumentMeta } from "../api/client";
import { displayName, imageDimensions, imagePosition } from "../lib/documents";

// The image viewer, shown by the document window for an image: the picture
// fills the body, fitted by default or at 100% (one image pixel to one CSS
// pixel) scrolling within the window; ← and → step through the owner's
// other images (useImageStepping). The window keeps its own header, with
// these controls in it.

export type ImageZoom = "fit" | "actual";

const TOGGLE = "inline-flex items-center rounded-md px-2 py-1 text-xs transition-colors";

/** Where the image sits among the owner's, its size in pixels, and Fit | 100%. */
export function ImageViewerControls({
  doc,
  images,
  zoom,
  onZoom,
}: {
  doc: DocumentMeta;
  images: readonly DocumentMeta[];
  zoom: ImageZoom;
  onZoom: (zoom: ImageZoom) => void;
}) {
  const facts = [imagePosition(doc, images), imageDimensions(doc)].filter(Boolean).join(" · ");
  return (
    <>
      {facts && (
        <span
          data-testid="image-facts"
          title={images.length > 1 ? "← and → step through the images" : undefined}
          className="hidden shrink-0 whitespace-nowrap text-xs text-slate-500 sm:inline"
        >
          {facts}
        </span>
      )}
      <div role="group" aria-label="Zoom" className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          aria-pressed={zoom === "fit"}
          title="Fit the image to the window"
          onClick={() => onZoom("fit")}
          className={`${TOGGLE} ${zoom === "fit" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}
        >
          Fit
        </button>
        <button
          type="button"
          aria-pressed={zoom === "actual"}
          title="Actual size"
          onClick={() => onZoom("actual")}
          className={`${TOGGLE} ${zoom === "actual" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}
        >
          100%
        </button>
      </div>
    </>
  );
}

/**
 * The picture itself, filling its positioned parent. Its URL carries the
 * revision, so an agent's replace loads the new file in place. Fit shrinks
 * it to the window, never enlarging a small one; 100% shows it at its own
 * size, scrolling when it is larger.
 */
export default function ImageView({ doc, zoom }: { doc: DocumentMeta; zoom: ImageZoom }) {
  const src = api.documents.imageUrl(doc.id, doc.revision);
  // The URL that failed to load: a new revision tries again.
  const [failed, setFailed] = useState<string | null>(null);
  const shown = displayName(doc);

  if (failed === src) {
    return <p className="px-4 py-4 text-sm text-slate-500 sm:px-8 sm:py-6">This image could not be loaded.</p>;
  }
  const onError = () => setFailed(src);

  if (zoom === "actual") {
    return (
      // Keyed by image, so stepping to another starts at its top left.
      <div
        key={doc.id}
        role="region"
        aria-label={`${shown} at 100%`}
        data-testid="image-scroll"
        tabIndex={0}
        className="absolute inset-0 overflow-auto focus:outline-none"
      >
        <div className="flex min-h-full w-max min-w-full p-4 sm:p-6">
          {/* Auto margins centre a smaller image and fall to 0 when it overflows, so no edge is cut off. */}
          <img src={src} alt={shown} onError={onError} data-zoom="actual" className="m-auto block h-auto max-h-none w-auto max-w-none shrink-0" />
        </div>
      </div>
    );
  }
  return (
    <div className="absolute inset-4 flex items-center justify-center sm:inset-6">
      <img src={src} alt={shown} onError={onError} data-zoom="fit" className="block h-auto max-h-full w-auto max-w-full object-contain" />
    </div>
  );
}
