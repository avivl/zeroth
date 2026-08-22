export type Route =
  | { name: "runs" }
  | { name: "run"; id: string }
  | { name: "agents" }
  | { name: "agent"; id: string }
  | { name: "approvals" }
  | { name: "memory" }
  | { name: "audit" };

export function parseHash(hash: string): Route {
  const path = (hash.replace(/^#/, "") || "/runs").replace(/\/+$/, "") || "/runs";
  const parts = path.split("/").filter(Boolean);
  if (parts[0] === "runs" && parts[1]) {
    return { name: "run", id: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "agents" && parts[1]) {
    return { name: "agent", id: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "agents") {
    return { name: "agents" };
  }
  if (parts[0] === "approvals") {
    return { name: "approvals" };
  }
  if (parts[0] === "memory") {
    return { name: "memory" };
  }
  if (parts[0] === "audit") {
    return { name: "audit" };
  }
  return { name: "runs" };
}

export function hrefFor(route: Route): string {
  switch (route.name) {
    case "run":
      return `#/runs/${encodeURIComponent(route.id)}`;
    case "agent":
      return `#/agents/${encodeURIComponent(route.id)}`;
    case "agents":
      return "#/agents";
    case "approvals":
      return "#/approvals";
    case "memory":
      return "#/memory";
    case "audit":
      return "#/audit";
    default:
      return "#/runs";
  }
}

export const NAV: Array<{ href: string; label: string; name: Route["name"] }> = [
  { href: "#/runs", label: "Runs", name: "runs" },
  { href: "#/agents", label: "Agents", name: "agents" },
  { href: "#/approvals", label: "Approvals", name: "approvals" },
  { href: "#/memory", label: "Memory", name: "memory" },
  { href: "#/audit", label: "Audit", name: "audit" },
];
