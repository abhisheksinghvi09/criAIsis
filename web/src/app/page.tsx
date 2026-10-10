"use client";

import Link from "next/link";
import { useState } from "react";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card, Check, Empty, SeverityBadge, Stat, formatTime } from "@/components/ui";
import { api } from "@/lib/api";
import { useCredentials } from "@/lib/session";
import { useResource } from "@/lib/useApi";
import type { IncidentSummary, TestInvestigationResult, WorkspaceSettings } from "@/lib/types";

export default function OverviewPage() {
  return (
    <AuthGate>
      <Overview />
    </AuthGate>
  );
}

function Overview() {
  const creds = useCredentials();
  const settings = useResource<WorkspaceSettings>(api.settings);
  const incidents = useResource<{ incidents: IncidentSummary[] | null }>(api.incidents);

  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<TestInvestigationResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function runCheck() {
    setRunning(true);
    setError(null);
    setResult(null);
    try {
      setResult(await api.testInvestigation(creds));
      incidents.reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "The setup check failed");
    } finally {
      setRunning(false);
    }
  }

  const s = settings.data;
  const rows = incidents.data?.incidents ?? [];
  const ready = s?.ready_to_investigate ?? false;

  return (
    <Shell
      title="Overview"
      subtitle={s ? s.name : "Loading workspace"}
      actions={
        ready ? (
          <button className="btn primary" onClick={runCheck} disabled={running} type="button">
            {running ? "Investigating..." : "Run setup check"}
          </button>
        ) : null
      }
    >
      {settings.error ? <div className="notice error">{settings.error}</div> : null}
      {error ? <div className="notice error">{error}</div> : null}

      {result ? (
        <div className="notice ok">
          Investigation complete in {result.elapsed}. {result.specialists} specialists reported;
          root cause classified as {result.classification}, owned by {result.owning_domain}.
          The full transcript was posted to your channel.
        </div>
      ) : null}

      {s && !ready ? (
        <div className="notice warn">
          <span>
            This workspace cannot investigate yet. Missing: {(s.missing ?? []).join(", ")}.{" "}
            <Link href="/setup">Finish setup</Link>
          </span>
        </div>
      ) : null}

      <div className="grid stats">
        <Stat label="Status" value={ready ? "Ready" : "Setup incomplete"} note={ready ? "Specialists can investigate" : "Add the missing credentials"} />
        <Stat label="Incidents" value={rows.length} note="Most recent first" />
        <Stat label="Posts to" value={s?.notifications.provider ?? "—"} note={s?.notifications.webhook_set ? "Destination configured" : "No destination set"} />
        <Stat label="Debate model" value={s?.llm.specialist_model ?? "—"} note="Runs on your own key" mono />
      </div>

      <div className="grid two">
        <Card title="Readiness" description="What this workspace needs before an alert can be investigated.">
          <div className="checklist">
            <Check done={s?.llm.api_key_set ?? false} label="Model credential" sub={s ? (s.llm.api_key_set ? "verified and stored" : "required for the debate") : undefined} />
            <Check done={s?.embeddings.api_key_set ?? false} label="Embedding credential" sub={s ? (s.embeddings.api_key_set ? "verified and stored" : "required for retrieval") : undefined} />
            <Check done={(s?.notifications.provider ?? "none") !== "none"} label="Notification destination" sub={s ? (s.notifications.provider === "none" ? "verdicts have nowhere to go" : `posting to ${s.notifications.provider}`) : undefined} />
          </div>
          <div className="row end">
            <Link className="btn secondary small" href="/setup">Open setup</Link>
          </div>
        </Card>

        <Card title="The mandate" description="The boundary this product is built around.">
          <p style={{ fontSize: 13.5, lineHeight: 1.65 }}>
            criAIsis performs deep read-only diagnostic investigation and never
            autonomous production remediation. Specialists may quote a destructive
            command from your runbooks; they have no channel to execute one.
          </p>
          <p style={{ fontSize: 12.5, color: "var(--text-muted)" }}>
            Retrieval is scoped to one workspace and one specialist at a time, so no
            persona can read another&apos;s runbooks and no tenant can read yours.
          </p>
        </Card>
      </div>

      <Card title="Recent incidents" description="Every investigation is an append-only record.">
        {incidents.loading ? (
          <Empty>Loading incidents…</Empty>
        ) : rows.length === 0 ? (
          <Empty>
            No incidents yet. Connect Grafana or CloudWatch on the Integrations page,
            or run a setup check above.
          </Empty>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Incident</th>
                  <th>Severity</th>
                  <th>Trigger</th>
                  <th>Status</th>
                  <th>Opened</th>
                </tr>
              </thead>
              <tbody>
                {rows.slice(0, 8).map((incident) => (
                  <tr key={incident.id}>
                    <td>
                      <Link href={`/incidents/${incident.id}`}>{incident.title}</Link>
                    </td>
                    <td><SeverityBadge severity={incident.severity} /></td>
                    <td className="mono">{incident.trigger}</td>
                    <td>
                      <span className={`badge ${incident.status === "resolved" ? "ok" : "accent"}`}>
                        {incident.status}
                      </span>
                    </td>
                    <td className="mono">{formatTime(incident.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </Shell>
  );
}
