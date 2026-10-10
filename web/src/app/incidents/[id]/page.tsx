"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback } from "react";
import { AuthGate } from "@/components/AuthGate";
import { SandboxReproductionPanel } from "@/components/SandboxReproductionPanel";
import { Shell } from "@/components/Shell";
import { Card, ClassificationBadge, Empty, PersonaBadge, SeverityBadge, formatTime } from "@/components/ui";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";
import { useResource } from "@/lib/useApi";
import type { Credentials, DebateTurn, IncidentDetail } from "@/lib/types";

export default function IncidentPage() {
  return (
    <AuthGate>
      <Incident />
    </AuthGate>
  );
}

function Incident() {
  const params = useParams<{ id: string }>();
  const id = params.id;
  const { creds } = useSession();

  const fetcher = useCallback((credentials: Credentials) => api.incident(credentials, id), [id]);
  const incident = useResource<IncidentDetail>(fetcher);
  const data = incident.data;

  return (
    <Shell
      title={data?.title ?? "Incident"}
      subtitle={data ? `${data.trigger} · opened ${formatTime(data.created_at)}` : undefined}
      actions={<Link className="btn subtle small" href="/incidents">Back to incidents</Link>}
    >
      {incident.error ? <div className="notice error">{incident.error}</div> : null}
      {incident.loading ? <Empty>Loading transcript…</Empty> : null}

      {data ? (
        <>
          <Card>
            <div className="row">
              <SeverityBadge severity={data.severity} />
              <span className={`badge ${data.status === "resolved" ? "ok" : "accent"}`}>{data.status}</span>
              <span className="badge muted mono">{data.trigger}</span>
            </div>
            <p style={{ fontSize: 13.5, lineHeight: 1.65 }}>{data.description}</p>
          </Card>

          <SandboxReproductionPanel
            incidentId={data.id}
            incidentStatus={data.status}
            credentials={creds}
          />

          {data.context ? <Evidence context={data.context} /> : null}

          {data.transcript.length === 0 ? (
            <Card title="Transcript">
              <Empty>No debate turns were recorded for this incident.</Empty>
            </Card>
          ) : (
            <div className="grid" style={{ gap: 14 }}>
              {data.transcript.map((turn) => (
                <Turn key={turn.id} turn={turn} />
              ))}
            </div>
          )}
        </>
      ) : null}
    </Shell>
  );
}

function Evidence({ context }: { context: NonNullable<IncidentDetail["context"]> }) {
  const metrics = Object.entries(context.metrics ?? {});
  return (
    <Card title="Ingested telemetry" description={`From ${context.provider}${context.alert_name ? ` · ${context.alert_name}` : ""}`}>
      {context.error_logs && context.error_logs.length > 0 ? (
        <div>
          <div className="turn-section-label">Error logs</div>
          <pre className="code">{context.error_logs.join("\n")}</pre>
        </div>
      ) : null}
      {context.stack_traces && context.stack_traces.length > 0 ? (
        <div>
          <div className="turn-section-label">Stack traces</div>
          <pre className="code">{context.stack_traces.join("\n\n")}</pre>
        </div>
      ) : null}
      {metrics.length > 0 ? (
        <div>
          <div className="turn-section-label">Metrics</div>
          <div className="table-wrap">
            <table>
              <tbody>
                {metrics.map(([name, val]) => (
                  <tr key={name}>
                    <td className="mono">{name}</td>
                    <td className="mono">{val}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
    </Card>
  );
}

const STAGE_LABEL: Record<number, string> = {
  1: "Stage 1 · parallel blast",
  2: "Stage 2 · consensus synthesis",
  3: "Stage 3 · follow-up",
};

function Turn({ turn }: { turn: DebateTurn }) {
  const meta = turn.metadata ?? {};
  const citations = turn.citations ?? meta.citations ?? [];

  return (
    <article className="turn">
      <header className="turn-head">
        <span className="stage">{STAGE_LABEL[turn.stage] ?? `Stage ${turn.stage}`}</span>
        {turn.persona ? <PersonaBadge persona={turn.persona.split(" ")[0]?.toLowerCase() ?? "muted"} /> : null}
        {turn.persona ? <strong style={{ fontSize: 13 }}>{turn.persona}</strong> : <strong style={{ fontSize: 13 }}>Incident Commander</strong>}
        {meta.confidence ? <span className="badge muted">{meta.confidence} confidence</span> : null}
        {meta.classification ? <ClassificationBadge value={meta.classification} /> : null}
        {meta.owning_domain ? <span className="badge accent">owner: {meta.owning_domain}</span> : null}
        <span className="mono" style={{ marginLeft: "auto", fontSize: 11, color: "var(--text-muted)" }}>
          {formatTime(turn.created_at)}
        </span>
      </header>

      <div className="turn-body">
        <p>{turn.content}</p>

        <Section label="Contradictions resolved" items={meta.contradictions} />
        <Section label="Next steps" items={meta.next_steps} />

        {meta.diagnostic_queries && meta.diagnostic_queries.length > 0 ? (
          <div>
            <div className="turn-section-label">Run these (read-only)</div>
            <pre className="code">{meta.diagnostic_queries.join("\n")}</pre>
          </div>
        ) : null}

        {citations.length > 0 ? (
          <div>
            <div className="turn-section-label">Cited runbooks</div>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {citations.map((citation) => (
                <div className="citation" key={citation.chunk_id}>
                  <div className="citation-title">{citation.document_title}</div>
                  <div>{citation.snippet.slice(0, 280)}{citation.snippet.length > 280 ? "…" : ""}</div>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </article>
  );
}

function Section({ label, items }: { label: string; items?: string[] }) {
  if (!items || items.length === 0) return null;
  return (
    <div>
      <div className="turn-section-label">{label}</div>
      <ul>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    </div>
  );
}
