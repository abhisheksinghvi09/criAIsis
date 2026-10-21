"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Card } from "@/components/ui";
import { api } from "@/lib/api";
import type { Credentials, SandboxAccess, SandboxReproduction, SandboxStatus } from "@/lib/types";

interface SandboxReproductionPanelProps {
  incidentId: string;
  incidentStatus: string;
  credentials: Credentials | null;
}

export function SandboxReproductionPanel({
  incidentId,
  incidentStatus,
  credentials,
}: SandboxReproductionPanelProps) {
  const [status, setStatus] = useState<SandboxStatus | "idle">("idle");
  const [reproduction, setReproduction] = useState<SandboxReproduction | null>(null);
  const [access, setAccess] = useState<SandboxAccess | null>(null);
  const [selectedScenario, setSelectedScenario] = useState<string>("oom_crashloop");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(false);

  const pollTimerRef = useRef<NodeJS.Timeout | null>(null);

  const isResolved = incidentStatus.toLowerCase() === "resolved";

  const clearPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    return () => clearPolling();
  }, [clearPolling]);

  const pollStatus = useCallback(
    async (reproId: string) => {
      if (!credentials) return;
      try {
        const res = await api.getSandbox(credentials, incidentId, reproId);
        if (!res) return;
        setReproduction(res);
        setStatus(res.status);

        if (res.status === "Ready") {
          clearPolling();
          try {
            const accessRes = await api.getSandboxAccess(credentials, incidentId, reproId);
            if (accessRes) setAccess(accessRes);
          } catch (err: unknown) {
            const msg = err instanceof Error ? err.message : "Failed to load container access details";
            setError(msg);
          }
        } else if (res.status === "UnmatchedFallback" || res.status === "Failed") {
          clearPolling();
        }
      } catch (err: unknown) {
        clearPolling();
        setStatus("Failed");
        const msg = err instanceof Error ? err.message : "Failed to query sandbox status";
        setError(msg);
      }
    },
    [credentials, incidentId, clearPolling]
  );

  const startPolling = useCallback(
    (reproId: string) => {
      clearPolling();
      pollTimerRef.current = setInterval(() => {
        pollStatus(reproId);
      }, 500);
    },
    [clearPolling, pollStatus]
  );

  const handleTrigger = async () => {
    if (!credentials) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api.triggerSandbox(credentials, incidentId);
      setReproduction(res);
      setStatus(res.status);
      if (res.status === "Provisioning") {
        startPolling(res.id);
      }
    } catch (err: unknown) {
      setStatus("Failed");
      const msg = err instanceof Error ? err.message : "Failed to trigger sandbox reproduction";
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  const handleSelectScenario = async () => {
    if (!credentials || !reproduction) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api.selectSandboxScenario(
        credentials,
        incidentId,
        reproduction.id,
        selectedScenario
      );
      setReproduction(res);
      setStatus(res.status);
      if (res.status === "Provisioning") {
        startPolling(res.id);
      }
    } catch (err: unknown) {
      setStatus("Failed");
      const msg = err instanceof Error ? err.message : "Failed to select scenario";
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  // If incident is not resolved, show notice
  if (!isResolved) {
    return (
      <Card title="Sandbox Reproduction">
        <p style={{ fontSize: 13, color: "var(--text-muted)" }}>
          Sandbox reproduction is available once an incident is marked resolved.
        </p>
      </Card>
    );
  }

  return (
    <Card
      title="Sandbox Reproduction"
      description="Replay this incident's telemetry and environment in an isolated throwaway container."
      actions={
        status === "idle" ? (
          <button
            type="button"
            className="btn primary small"
            onClick={handleTrigger}
            disabled={loading || !credentials}
            data-testid="reproduce-sandbox-btn"
          >
            {loading ? "Starting..." : "Reproduce in Sandbox"}
          </button>
        ) : null
      }
    >
      {status === "idle" && (
        <p style={{ fontSize: 13, color: "var(--text-muted)" }}>
          Extracts captured error logs, stack traces, and metric tags from the incident context to provision an isolated reproduction environment.
        </p>
      )}

      {status === "Provisioning" && (
        <div aria-live="polite" className="notice info" data-testid="sandbox-provisioning-notice">
          <span className="badge accent" style={{ marginRight: 8 }}>
            Provisioning
          </span>
          Mapping incident context and spinning up throwaway container...
        </div>
      )}

      {status === "Ready" && (
        <div aria-live="polite" className="notice ok" data-testid="sandbox-ready-notice">
          <div
            className="row"
            style={{
              justifyContent: "space-between",
              alignItems: "center",
              width: "100%",
              flexWrap: "wrap",
              gap: 10,
            }}
          >
            <div>
              <span className="badge ok" style={{ marginRight: 8 }}>
                Ready
              </span>
              Container {reproduction?.container_ref ?? "active"}
              {reproduction?.scenario_id ? ` · Scenario: ${reproduction.scenario_id}` : ""}
            </div>
            <div className="row" style={{ gap: 8 }}>
              {access?.access_url ? (
                <a
                  className="btn accent small"
                  href={access.access_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="open-sandbox-btn"
                >
                  Open Sandbox
                </a>
              ) : null}
              {access?.logs_url ? (
                <a
                  className="btn subtle small"
                  href={access.logs_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="view-logs-btn"
                >
                  View Logs
                </a>
              ) : null}
            </div>
          </div>
        </div>
      )}

      {status === "UnmatchedFallback" && (
        <div aria-live="polite" className="notice warn" data-testid="sandbox-fallback-notice">
          <div style={{ marginBottom: 12 }}>
            <span className="badge medium" style={{ marginRight: 8 }}>
              No Auto-Match
            </span>
            <strong>Could not automatically match scenario</strong>
            <p style={{ fontSize: 13, marginTop: 4 }}>
              Incident telemetry did not match an automated archetype. Select a scenario manually to start reproduction:
            </p>
          </div>
          <div className="row" style={{ gap: 10, alignItems: "center" }}>
            <label htmlFor="scenario-select" className="sr-only" style={{ display: "none" }}>
              Select reproduction scenario
            </label>
            <select
              id="scenario-select"
              value={selectedScenario}
              onChange={(e) => setSelectedScenario(e.target.value)}
              aria-label="Select reproduction scenario"
              style={{
                padding: "6px 10px",
                borderRadius: 6,
                fontSize: 13,
                border: "1px solid var(--border)",
                background: "var(--surface)",
                color: "var(--text)",
              }}
              data-testid="sandbox-scenario-select"
            >
              <option value="oom_crashloop">oom_crashloop (Out of Memory spike)</option>
              <option value="db_connection_exhaustion">db_connection_exhaustion (Connection pool exhaustion)</option>
              <option value="auth_latency_spike">auth_latency_spike (Upstream auth degradation)</option>
            </select>
            <button
              type="button"
              className="btn primary small"
              onClick={handleSelectScenario}
              disabled={loading}
              data-testid="start-scenario-btn"
            >
              {loading ? "Starting..." : "Start with this scenario"}
            </button>
          </div>
        </div>
      )}

      {status === "Failed" && (
        <div aria-live="polite" className="notice error" data-testid="sandbox-error-notice">
          <div
            className="row"
            style={{
              justifyContent: "space-between",
              alignItems: "center",
              width: "100%",
              flexWrap: "wrap",
              gap: 10,
            }}
          >
            <div>
              <span className="badge critical" style={{ marginRight: 8 }}>
                Failed
              </span>
              <strong>Provisioning Error</strong>
              <p style={{ fontSize: 13, marginTop: 4 }}>{error ?? "Failed to provision sandbox container."}</p>
            </div>
            <button
              type="button"
              className="btn subtle small"
              onClick={handleTrigger}
              disabled={loading}
              data-testid="sandbox-retry-btn"
            >
              Try Again
            </button>
          </div>
        </div>
      )}
    </Card>
  );
}
