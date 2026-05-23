import { useEffect, useState } from "react";
import { Routes, Route, Link } from "react-router-dom";
import { ScenariosPage } from "./pages/ScenariosPage";
import { RunsPage } from "./pages/RunsPage";
import { RunDetailPage } from "./pages/RunDetailPage";
import { TargetsPage } from "./pages/TargetsPage";
import { SchedulesPage } from "./pages/SchedulesPage";
import { setAuthToken } from "./api/client";

// Token key used by `dlh login` (session exchange) and the dev fake-token path.
const TOKEN_KEY = "dlh-token";

export default function App() {
  const [ready, setReady] = useState(false);
  const [authErr, setAuthErr] = useState<string | null>(null);

  useEffect(() => {
    fetch("/api/auth/info")
      .then((r) => r.json())
      .then((info: { authDisabled?: boolean }) => {
        if (info.authDisabled) {
          // DLH_AUTH_DISABLED=true: inject a fake token so the middleware is
          // satisfied without a real OIDC flow.
          setAuthToken("fake:admin:admin@local:dlh-admin");
        } else {
          // Production: look for a session token stored by `dlh login` /
          // the POST /api/auth/exchange flow.
          const tok = localStorage.getItem(TOKEN_KEY);
          if (tok) {
            setAuthToken(tok);
          } else {
            setAuthErr("Not authenticated. Run `dlh login` or set a session token.");
            return;
          }
        }
        setReady(true);
      })
      .catch((e) => setAuthErr(String(e)));
  }, []);

  if (authErr) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="max-w-md rounded border border-rose-200 bg-rose-50 px-6 py-4 text-rose-800">
          {authErr}
        </p>
      </div>
    );
  }

  if (!ready) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="text-slate-500">Connecting…</p>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-6xl gap-4 px-6 py-3 text-sm">
          <Link to="/" className="font-semibold">dlh-controlplane</Link>
          <Link to="/scenarios" className="text-slate-600 hover:text-slate-900">Scenarios</Link>
          <Link to="/runs" className="text-slate-600 hover:text-slate-900">Runs</Link>
          <Link to="/targets" className="text-slate-600 hover:text-slate-900">Targets</Link>
          <Link to="/schedules" className="text-slate-600 hover:text-slate-900">Schedules</Link>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<RunsPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:id" element={<RunDetailPage />} />
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/schedules" element={<SchedulesPage />} />
        </Routes>
      </main>
    </div>
  );
}
