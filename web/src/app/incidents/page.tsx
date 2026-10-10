"use client";

import Link from "next/link";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card, Empty, SeverityBadge, formatTime } from "@/components/ui";
import { api } from "@/lib/api";
import { useResource } from "@/lib/useApi";
import type { IncidentSummary } from "@/lib/types";

export default function IncidentsPage() {
  return (
    <AuthGate>
      <Incidents />
    </AuthGate>
  );
}

function Incidents() {
  const incidents = useResource<{ incidents: IncidentSummary[] | null }>(api.incidents);
  const rows = incidents.data?.incidents ?? [];

  return (
    <Shell title="Incidents" subtitle="Append-only transcripts; citations survive runbook edits">
      {incidents.error ? <div className="notice error">{incidents.error}</div> : null}

      <Card>
        {incidents.loading ? (
          <Empty>Loading incidents…</Empty>
        ) : rows.length === 0 ? (
          <Empty>No incidents recorded yet.</Empty>
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
                {rows.map((incident) => (
                  <tr key={incident.id}>
                    <td>
                      <Link href={`/incidents/${incident.id}`}>{incident.title}</Link>
                      <div style={{ fontSize: 12, color: "var(--text-muted)", marginTop: 2 }}>
                        {incident.description.slice(0, 110)}
                        {incident.description.length > 110 ? "…" : ""}
                      </div>
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
