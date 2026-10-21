"use client";

import { useState } from "react";
import { Mark } from "./Mark";
import { api, ApiError } from "@/lib/api";
import { useSession } from "@/lib/session";

/** AuthGate holds the sign-in form and renders children only once a workspace
 *  admin key has been proven against that workspace. */
export function AuthGate({ children }: { children: React.ReactNode }) {
  const { creds, ready, signIn } = useSession();
  const [workspace, setWorkspace] = useState("");
  const [adminKey, setAdminKey] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (!ready) return null;
  if (creds) return <>{children}</>;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);

    const candidate = { workspace: workspace.trim(), adminKey: adminKey.trim() };
    try {
      // Verify before storing, so a wrong key never lands in session storage.
      await api.settings(candidate);
      signIn(candidate);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? "That workspace and admin key combination was not accepted."
          : err instanceof Error
            ? err.message
            : "Could not reach the API.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="signin">
      <div className="signin-card">
        <div className="signin-brand">
          <Mark size={30} />
          <div>
            <div style={{ fontSize: 19, fontWeight: 600, letterSpacing: "-0.02em" }}>criAIsis</div>
            <div className="mono" style={{ fontSize: 10, letterSpacing: "0.14em", textTransform: "uppercase", color: "var(--text-muted)" }}>
              War Room
            </div>
          </div>
        </div>

        <form className="card" onSubmit={submit}>
          <div className="card-head">
            <div>
              <h2>Sign in to your workspace</h2>
              <p>Use the admin key issued when this workspace was created.</p>
            </div>
          </div>
          <div className="card-body">
            {error ? <div className="notice error">{error}</div> : null}

            <div className="field">
              <label htmlFor="workspace">Workspace</label>
              <input
                id="workspace"
                className="input mono"
                value={workspace}
                onChange={(e) => setWorkspace(e.target.value)}
                placeholder="acme"
                autoComplete="organization"
                required
              />
            </div>

            <div className="field">
              <label htmlFor="adminKey">Admin key</label>
              <input
                id="adminKey"
                className="input mono"
                type="password"
                value={adminKey}
                onChange={(e) => setAdminKey(e.target.value)}
                placeholder="shown once when the workspace was created"
                autoComplete="current-password"
                required
              />
              <span className="hint">
                Held for this browser session only. It administers this workspace and no other.
              </span>
            </div>

            <div className="row end">
              <button className="btn primary" type="submit" disabled={busy}>
                {busy ? "Checking..." : "Sign in"}
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
