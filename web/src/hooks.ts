import { useCallback, useEffect, useState } from "react";
import { parseHash, type Route } from "./routes";

export function useHashRoute(): [Route, (hash: string) => void] {
  const [route, setRoute] = useState<Route>(() => {
    if (typeof window === "undefined") {
      return { name: "runs" };
    }
    return parseHash(window.location.hash);
  });
  useEffect(() => {
    const onHash = () => setRoute(parseHash(window.location.hash));
    window.addEventListener("hashchange", onHash);
    if (!window.location.hash) {
      window.location.hash = "#/runs";
    }
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  const go = useCallback((hash: string) => {
    window.location.hash = hash;
  }, []);
  return [route, go];
}

export type ThemeName = "light" | "dark";

export function useTheme(): [ThemeName, (next: ThemeName) => void] {
  const [theme, setTheme] = useState<ThemeName>(() => {
    if (typeof window === "undefined") {
      return "light";
    }
    const stored = window.localStorage.getItem("zeroth-theme");
    if (stored === "dark" || stored === "light") {
      return stored;
    }
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });
  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    window.localStorage.setItem("zeroth-theme", theme);
  }, [theme]);
  return [theme, setTheme];
}
