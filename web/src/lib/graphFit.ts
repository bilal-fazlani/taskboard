/**
 * When the Dependencies view fits the graph to the viewport, with no React
 * and no DOM.
 *
 * A fit is requested on first load and by the user's changes that lay out a
 * different graph (Graph.tsx says which). It is then pending until it can
 * frame the real graph: the tickets laid out, every card measured and the
 * viewport's size known. It also waits for the document to be shown. A graph
 * opened in a background tab is not measured there, and once a live refresh
 * could re-render it, "the next render" might come long after the tab was
 * first looked at; so a hidden page's fit lands on the render that shows it.
 *
 * The user's own pan or zoom, the Fit button included, settles a pending fit
 * for good: a fit is never applied over a graph the user has moved. Only a
 * new request, which only the user's changes make, fits it again.
 */

/** What a render knows about whether a fit could frame the graph now. */
export interface FitMoment {
  /** The document is shown, not in a background tab or a minimised window. */
  visible: boolean;
  /** There are cards on the graph to frame. */
  hasGraph: boolean;
  /** Every card on the graph has been measured. */
  measured: boolean;
  /** The viewport's size is known. */
  hasViewport: boolean;
}

export type FitEvent =
  /** First load, or a user change that lays out a different graph. */
  | { type: "requested" }
  /** The user panned or zoomed, or pressed Fit. */
  | { type: "userMoved" }
  /** A render, for any reason: a measurement, a live refresh, the tab shown. */
  | { type: "rendered"; moment: FitMoment };

export interface FitStep {
  /** Whether a fit is still waiting to be applied. */
  pending: boolean;
  /** Whether to fit the graph now. */
  fit: boolean;
}

export function stepFit(pending: boolean, event: FitEvent): FitStep {
  switch (event.type) {
    case "requested":
      return { pending: true, fit: false };
    case "userMoved":
      return { pending: false, fit: false };
    case "rendered": {
      const { visible, hasGraph, measured, hasViewport } = event.moment;
      const fit = pending && visible && hasGraph && measured && hasViewport;
      return { pending: pending && !fit, fit };
    }
  }
}
