import { createApi } from "./api/client";
import { NAV, hrefFor } from "./routes";
import { useHashRoute, useTheme } from "./hooks";
import {
  AgentConfigView,
  AgentsView,
  ApprovalsView,
  AuditView,
  MemoryView,
  RunDetailView,
  RunsView,
} from "./views";
import { CrossExamNotes } from "./components/ui";

const api = createApi();

export { CrossExamNotes };

export function App() {
  const [route] = useHashRoute();
  const [theme, setTheme] = useTheme();

  return (
    <div className="app">
      <nav className="sidebar" aria-label="Primary">
        <div className="brand">
          <h1>Zeroth</h1>
        </div>
        {NAV.map((item) => {
          const current =
            route.name === item.name ||
            (item.name === "runs" && route.name === "run") ||
            (item.name === "agents" && route.name === "agent");
          return (
            <a
              key={item.href}
              className="nav-link"
              href={item.href}
              aria-current={current ? "page" : undefined}
            >
              {item.label}
            </a>
          );
        })}
        <button
          type="button"
          className="btn theme-toggle"
          onClick={() => setTheme(theme === "light" ? "dark" : "light")}
          aria-label="Toggle color theme"
        >
          {theme === "light" ? "Dark" : "Light"} theme
        </button>
      </nav>
      <main className="workspace">
        {route.name === "runs" ? <RunsView api={api} /> : null}
        {route.name === "run" ? <RunDetailView api={api} id={route.id} /> : null}
        {route.name === "agents" ? <AgentsView api={api} /> : null}
        {route.name === "agent" ? <AgentConfigView api={api} id={route.id} /> : null}
        {route.name === "approvals" ? <ApprovalsView api={api} /> : null}
        {route.name === "memory" ? <MemoryView api={api} /> : null}
        {route.name === "audit" ? <AuditView api={api} /> : null}
      </main>
    </div>
  );
}

export function defaultHref(): string {
  return hrefFor({ name: "runs" });
}
