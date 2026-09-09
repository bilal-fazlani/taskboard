import { BrowserRouter, Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Board from "./pages/Board";
import Projects from "./pages/Projects";
import Tickets from "./pages/Tickets";
import Labels from "./pages/Labels";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Board />} />
          <Route path="projects" element={<Projects />} />
          <Route path="tickets" element={<Tickets />} />
          <Route path="labels" element={<Labels />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
