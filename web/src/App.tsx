import { BrowserRouter, Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Board from "./pages/Board";
import Graph from "./pages/Graph";
import Projects from "./pages/Projects";
import Tickets from "./pages/Tickets";
import Labels from "./pages/Labels";
import Epics from "./pages/Epics";

// Routes: the views at /, /kanban and /table; Epics, Projects and Labels at
// /epics, /projects and /labels. The old paths (/board, /tickets, /graph)
// are gone on purpose, with no redirects.
export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Graph />} />
        <Route path="kanban" element={<Board />} />
        <Route path="table" element={<Tickets />} />
        <Route path="epics" element={<Epics />} />
        <Route path="projects" element={<Projects />} />
        <Route path="labels" element={<Labels />} />
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
