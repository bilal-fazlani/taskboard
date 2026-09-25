import { useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { api, type DocumentMeta } from "../api/client";
import { IMAGE_SCROLL } from "../hooks/useImageStepping";
import { adjacentImage, displayName, imageDimensions, imagePosition } from "../lib/documents";

// The image viewer, shown by the document window for an image: the picture
// fills the body, fitted by default or at 100% (one image pixel to one CSS
// pixel) scrolling within the window. Previous and next buttons either side
// of it, and ← and → (useImageStepping), step through the owner's other
// images; a footer says so. The window keeps its own header, with the
// controls here in it.

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
export default function ImageView({
  doc,
  zoom,
  images,
  onStep,
  paused = false,
}: {
  doc: DocumentMeta;
  zoom: ImageZoom;
  images: readonly DocumentMeta[];
  /** Show another of the owner's images; without it there are no buttons. */
  onStep?: (doc: DocumentMeta) => void;
  /** A rename field or a confirm is up: the buttons do nothing. */
  paused?: boolean;
}) {
  const src = api.documents.imageUrl(doc.id, doc.revision);
  // The URL that failed to load: a new revision tries again.
  const [failed, setFailed] = useState<string | null>(null);
  const shown = displayName(doc);
  const stepping = onStep !== undefined && images.length > 1 && images.some((d) => d.id === doc.id);
  const onError = () => setFailed(src);

  let picture: React.ReactNode;
  if (failed === src) {
    picture = <p className="px-4 py-4 text-sm text-slate-500 sm:px-8 sm:py-6">This image could not be loaded.</p>;
  } else if (zoom === "actual") {
    picture = (
      // Keyed by image, so stepping to another starts at its top left.
      <div
        key={doc.id}
        role="region"
        aria-label={`${shown} at 100%`}
        data-testid="image-scroll"
        {...{ [IMAGE_SCROLL]: "" }}
        tabIndex={0}
        className="absolute inset-0 overflow-auto focus:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-blue-500"
      >
        <div className={`flex min-h-full w-max min-w-full py-4 sm:py-6 ${stepping ? "px-16" : "px-4 sm:px-6"}`}>
          {/* Auto margins centre a smaller image and fall to 0 when it overflows, so no edge is cut off. */}
          <img src={src} alt={shown} onError={onError} data-zoom="actual" className="m-auto block h-auto max-h-none w-auto max-w-none shrink-0" />
        </div>
      </div>
    );
  } else {
    picture = (
      // With the buttons, the picture keeps clear of them.
      <div className={`absolute flex items-center justify-center ${stepping ? "inset-x-16 inset-y-4 sm:inset-y-6" : "inset-4 sm:inset-6"}`}>
        <img src={src} alt={shown} onError={onError} data-zoom="fit" className="block h-auto max-h-full w-auto max-w-full object-contain" />
      </div>
    );
  }
  if (!stepping) return picture;
  return (
    <>
      {picture}
      <StepButton label="Previous image" to={adjacentImage(doc, images, -1)} paused={paused} onStep={onStep} side="left" />
      <StepButton label="Next image" to={adjacentImage(doc, images, 1)} paused={paused} onStep={onStep} side="right" />
    </>
  );
}

const STEP_BUTTON =
  "absolute top-1/2 z-10 -translate-y-1/2 rounded-full border border-slate-700 bg-slate-900/80 p-2 text-slate-300 shadow-lg transition-colors hover:bg-slate-800 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 aria-disabled:cursor-default aria-disabled:opacity-30 aria-disabled:hover:bg-slate-900/80 aria-disabled:hover:text-slate-300";

// Disabled with aria-disabled rather than `disabled`, so the button keeps
// focus when a step lands on the first or last image and stays in the Tab
// order; a click then does nothing.
function StepButton({
  label,
  to,
  paused,
  onStep,
  side,
}: {
  label: string;
  to: DocumentMeta | null;
  paused: boolean;
  onStep: (doc: DocumentMeta) => void;
  side: "left" | "right";
}) {
  const off = to === null || paused;
  const Icon = side === "left" ? ChevronLeft : ChevronRight;
  return (
    <button
      type="button"
      aria-label={label}
      title={to ? `${label}: ${displayName(to)}` : label}
      aria-disabled={off || undefined}
      onClick={() => {
        if (to && !paused) onStep(to);
      }}
      className={`${STEP_BUTTON} ${side === "left" ? "left-3" : "right-3"}`}
    >
      <Icon aria-hidden="true" className="h-5 w-5" />
    </button>
  );
}

/** "← → move between this ticket's images · Esc closes · Back closes". */
export function ImageViewerFooter({ images, ownerNoun }: { images: readonly DocumentMeta[]; ownerNoun: string }) {
  const parts = ["Esc closes", "Back closes"];
  if (images.length > 1) parts.unshift(`← → move between this ${ownerNoun}'s images`);
  return (
    <footer
      data-testid="image-hint"
      className="shrink-0 border-t border-slate-800 px-4 py-2 text-center text-[11px] text-slate-500 sm:px-6"
    >
      {parts.join(" · ")}
    </footer>
  );
}
