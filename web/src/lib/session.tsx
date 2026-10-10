"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { Credentials } from "./types";

const STORAGE_KEY = "criaisis.session";

interface SessionValue {
  creds: Credentials | null;
  /** null until the stored session has been read, so the shell does not flash
   *  the sign-in screen for an already-authenticated operator. */
  ready: boolean;
  signIn: (creds: Credentials) => void;
  signOut: () => void;
}

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [creds, setCreds] = useState<Credentials | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    // sessionStorage, not localStorage: the admin key is a bearer credential and
    // should not outlive the browser session.
    const stored = window.sessionStorage.getItem(STORAGE_KEY);
    if (stored) {
      try {
        setCreds(JSON.parse(stored) as Credentials);
      } catch {
        window.sessionStorage.removeItem(STORAGE_KEY);
      }
    }
    setReady(true);
  }, []);

  const signIn = useCallback((next: Credentials) => {
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    setCreds(next);
  }, []);

  const signOut = useCallback(() => {
    window.sessionStorage.removeItem(STORAGE_KEY);
    setCreds(null);
  }, []);

  const value = useMemo(() => ({ creds, ready, signIn, signOut }), [creds, ready, signIn, signOut]);
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionValue {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used inside SessionProvider");
  return ctx;
}

/** useCredentials is for views that only render once authenticated. */
export function useCredentials(): Credentials {
  const { creds } = useSession();
  if (!creds) throw new Error("useCredentials used outside an authenticated view");
  return creds;
}
