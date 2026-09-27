import { BrowserRouter, Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Board from "./pages/Board";
import Graph from "./pages/Graph";
import Projects from "./pages/Projects";
import Tickets from "./pages/Tickets";
import Labels from "./pages/Labels";
import Epics from "./pages/Epics";
import Now from "./pages/Now";
import NotFound from "./pages/NotFound";
import Activity from "./pages/Activity";
import Home from "./pages/Home";

// Routes: Now, the home page, at /now; the views at /dependencies, /kanban
// and /table; Epics, Projects and Labels at /epics, /projects and /labels.
// / itself only redirects (see Home): to the last ticket view when it names a
// ticket, which keeps every stored /?ticket= link working, and to Now
// otherwise. The old paths (/board, /tickets, /graph) are gone on purpose,
// with no redirects: like any other unknown path, they show the not-found
// page inside the layout.
export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="dependencies" element={<Graph />} />
        <Route path="now" element={<Now />} />
        <Route path="kanban" element={<Board />} />
        <Route path="table" element={<Tickets />} />
        <Route path="activity" element={<Activity />} />
        <Route path="epics" element={<Epics />} />
        <Route path="projects" element={<Projects />} />
        <Route path="labels" element={<Labels />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  );
}
