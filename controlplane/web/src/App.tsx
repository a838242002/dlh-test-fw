import { Routes, Route, Link } from "react-router-dom";
import { ScenariosPage } from "./pages/ScenariosPage";
import { RunsPage } from "./pages/RunsPage";
import { RunDetailPage } from "./pages/RunDetailPage";
import { TargetsPage } from "./pages/TargetsPage";

export default function App() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-6xl gap-4 px-6 py-3 text-sm">
          <Link to="/" className="font-semibold">dlh-controlplane</Link>
          <Link to="/scenarios" className="text-slate-600 hover:text-slate-900">Scenarios</Link>
          <Link to="/runs" className="text-slate-600 hover:text-slate-900">Runs</Link>
          <Link to="/targets" className="text-slate-600 hover:text-slate-900">Targets</Link>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<RunsPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:id" element={<RunDetailPage />} />
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </main>
    </div>
  );
}
