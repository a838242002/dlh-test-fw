import { Routes, Route, Link } from "react-router-dom";

export default function App() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-6xl gap-4 px-6 py-3 text-sm">
          <Link to="/" className="font-semibold">dlh-controlplane</Link>
          <Link to="/scenarios" className="text-slate-600 hover:text-slate-900">Scenarios</Link>
          <Link to="/runs" className="text-slate-600 hover:text-slate-900">Runs</Link>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/scenarios" element={<Placeholder name="Scenarios" />} />
          <Route path="/runs" element={<Placeholder name="Runs" />} />
          <Route path="/runs/:id" element={<Placeholder name="Run detail" />} />
        </Routes>
      </main>
    </div>
  );
}

function Home() {
  return <p>Phase B viewer. Pick Scenarios or Runs above.</p>;
}

function Placeholder({ name }: { name: string }) {
  return <p>{name} — pending Task 13 implementation.</p>;
}
