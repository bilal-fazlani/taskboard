import { Navigate, useLocation } from "react-router-dom";
import { readLastView } from "../lib/lastView";
import { TICKET_PARAM } from "../lib/ticketParam";

// The home path, /, has no page of its own: it sends you on, replacing itself
// in the history so Back never lands on it again.
//
// With a `ticket` parameter it is a ticket's canonical link (the server builds
// every ticket url as /?ticket=<KEY>), so it opens the last ticket view shown,
// which opens the ticket. Without one it is the home page, Now. Either way the
// whole query goes along, so a link's filters and open document survive.
export default function Home() {
  const { search, hash } = useLocation();
  const opensTicket = new URLSearchParams(search).has(TICKET_PARAM);
  const pathname = opensTicket ? readLastView() : "/now";
  return <Navigate to={{ pathname, search, hash }} replace />;
}
