import { createContext, useContext, useMemo, useState } from "react";
import { ImageOff } from "lucide-react";
import Markdown, { type Components } from "react-markdown";
import type { DocumentMeta } from "../api/client";
import { displayName } from "../lib/documents";
import { remarkSpacedImageRefs, resolveImageRef } from "../lib/imageRefs";

const REMARK_PLUGINS = [remarkSpacedImageRefs];

type Owner = { documents: readonly DocumentMeta[] | null; ownerNoun: string };
// The owner reaches each image through context, so the renderer below stays
// one component and a live reload of the documents never remounts an image.
const OwnerContext = createContext<Owner>({ documents: null, ownerNoun: "ticket" });

// Stands where an image would be when the name finds none of the owner's
// images (or the image could not be loaded): a quiet marker naming what is
// missing, never a broken-image icon.
function MissingImage({ name, alt, ownerNoun }: { name: string; alt?: string; ownerNoun: string }) {
  const label = name || alt || "";
  return (
    <span
      role="img"
      aria-label={label ? `Missing image: ${label}` : "Missing image"}
      title={name ? `This ${ownerNoun} has no image called "${name}".` : "This image can't be shown."}
      data-testid="missing-image"
      className="inline-flex max-w-full items-center gap-1.5 rounded-md border border-dashed border-slate-600 bg-slate-800/60 px-2 py-1 align-middle text-xs text-slate-400"
    >
      <ImageOff aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">{label ? `Missing image: ${label}` : "Missing image"}</span>
    </span>
  );
}

function OwnerImage({ src, alt, title }: { src?: string; alt?: string; title?: string }) {
  const { documents, ownerNoun } = useContext(OwnerContext);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  if (documents === null) {
    // The owner's documents are still loading: hold the place quietly.
    return <span data-testid="image-loading" className="text-xs text-slate-600">{alt || ""}</span>;
  }
  const target = resolveImageRef(src, documents);
  if (target.kind === "absolute") return <img src={target.src} alt={alt ?? ""} title={title} />;
  if (target.kind === "missing" || failedSrc === target.src) {
    const name = target.kind === "missing" ? target.name : displayName(target.doc);
    return <MissingImage name={name} alt={alt} ownerNoun={ownerNoun} />;
  }
  const { doc } = target;
  return (
    <img
      src={target.src}
      alt={alt ?? ""}
      title={title}
      width={doc.width || undefined}
      height={doc.height || undefined}
      loading="lazy"
      onError={() => setFailedSrc(target.src)}
    />
  );
}

const COMPONENTS: Components = {
  img: ({ src, alt, title }) => <OwnerImage src={typeof src === "string" ? src : undefined} alt={alt} title={title} />,
};

/**
 * Markdown written by or for an owner (a ticket's description, a ticket's or
 * an epic's markdown document), showing the owner's images by name
 * (lib/imageRefs.ts). `documents` are the owner's documents, null while they
 * load.
 */
export default function OwnerMarkdown({
  children,
  documents,
  ownerNoun = "ticket",
}: {
  children: string;
  documents: readonly DocumentMeta[] | null;
  ownerNoun?: string;
}) {
  const owner = useMemo(() => ({ documents, ownerNoun }), [documents, ownerNoun]);
  return (
    <OwnerContext.Provider value={owner}>
      <Markdown remarkPlugins={REMARK_PLUGINS} components={COMPONENTS}>
        {children}
      </Markdown>
    </OwnerContext.Provider>
  );
}
