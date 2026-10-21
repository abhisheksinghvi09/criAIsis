"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

export type Mode = "light" | "dark";

const STORAGE_KEY = "criaisis.mode";

interface ThemeValue {
  mode: Mode;
  toggle: () => void;
}

const ThemeContext = createContext<ThemeValue | null>(null);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  // Light is the approved first-use mode. A stored preference wins; the OS
  // setting is deliberately not consulted, so first use is predictable.
  const [mode, setMode] = useState<Mode>("light");

  useEffect(() => {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === "dark" || stored === "light") setMode(stored);
  }, []);

  useEffect(() => {
    // The mode is an attribute on <html>, so colours and fonts follow it without
    // remounting: the active route, form state and filters survive the switch.
    document.documentElement.setAttribute("data-theme", mode);
  }, [mode]);

  const toggle = useCallback(() => {
    setMode((current) => {
      const next = current === "light" ? "dark" : "light";
      window.localStorage.setItem(STORAGE_KEY, next);
      return next;
    });
  }, []);

  const value = useMemo(() => ({ mode, toggle }), [mode, toggle]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
