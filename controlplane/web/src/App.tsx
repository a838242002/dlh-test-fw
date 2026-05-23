import { useEffect, useState } from "react";
import { Routes, Route, NavLink } from "react-router-dom";
import { Activity, Clock, Crosshair, LayoutGrid, Moon, Sun } from "lucide-react";
import { ScenariosPage } from "./pages/ScenariosPage";
import { RunsPage } from "./pages/RunsPage";
import { RunDetailPage } from "./pages/RunDetailPage";
import { TargetsPage } from "./pages/TargetsPage";
import { SchedulesPage } from "./pages/SchedulesPage";
import { setAuthToken } from "./api/client";
import { useTheme } from "@/lib/theme";
import { Toaster } from "@/components/ui/sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const TOKEN_KEY = "dlh-token";
const FAKE_TOKEN = "fake:admin:admin@local:dlh-admin";

const NAV = [
  { to: "/runs", label: "Runs", Icon: Activity },
  { to: "/scenarios", label: "Scenarios", Icon: LayoutGrid },
  { to: "/targets", label: "Targets", Icon: Crosshair },
  { to: "/schedules", label: "Schedules", Icon: Clock },
];

function ThemeToggle() {
  const { theme, toggle } = useTheme();
  return (
    <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
      {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
}

export default function App() {
  const [ready, setReady] = useState(false);
  const [authErr, setAuthErr] = useState<string | null>(null);
  const [identity, setIdentity] = useState<string>("");

  useEffect(() => {
    fetch("/api/auth/info")
      .then((r) => r.json())
      .then((info: { authDisabled?: boolean }) => {
        if (info.authDisabled) {
          setAuthToken(FAKE_TOKEN);
          setIdentity("admin@local");
        } else {
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
      <div className="flex min-h-screen items-center justify-center bg-background">
        <p className="max-w-md rounded border border-destructive/40 bg-destructive/10 px-6 py-4 text-destructive">
          {authErr}
        </p>
      </div>
    );
  }

  if (!ready) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <p className="text-muted-foreground">Connecting…</p>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b bg-card">
        <nav className="mx-auto flex max-w-7xl items-center gap-4 px-6 py-3 text-sm">
          <span className="font-semibold text-primary">◆ dlh</span>
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1 transition-colors",
                  isActive
                    ? "bg-primary/15 font-medium text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                )
              }
            >
              <n.Icon className="h-4 w-4" />
              {n.label}
            </NavLink>
          ))}
          <div className="ml-auto flex items-center gap-3">
            {identity && <span className="text-xs text-muted-foreground">{identity}</span>}
            <ThemeToggle />
          </div>
        </nav>
      </header>
      <main className="mx-auto max-w-7xl px-6 py-8">
        <Routes>
          <Route path="/" element={<RunsPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:id" element={<RunDetailPage />} />
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/schedules" element={<SchedulesPage />} />
        </Routes>
      </main>
      <Toaster />
    </div>
  );
}
