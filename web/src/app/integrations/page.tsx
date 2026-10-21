"use client";

import { useState } from "react";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card } from "@/components/ui";
import { api } from "@/lib/api";
import { useCredentials } from "@/lib/session";

export default function IntegrationsPage() {
  return (
    <AuthGate>
      <Integrations />
    </AuthGate>
  );
}

function Integrations() {
  const creds = useCredentials();
  const [token, setToken] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The dashboard is proxied to the API, so the browser origin is the address an
  // operator would expose through a tunnel.
  const origin = typeof window === "undefined" ? "https://your-host" : window.location.origin;
  const endpoint = (provider: string) =>
    `${origin}/api/v1/integrations/alerts/${provider}?workspace=${encodeURIComponent(creds.workspace)}`;

  async function rotate() {
    setBusy(true);
    setError(null);
    try {
      setToken((await api.rotateAlertToken(creds)).alert_token);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not rotate the token");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Shell title="Integrations" subtitle="Where incidents come from">
      {error ? <div className="notice error">{error}</div> : null}

      <Card
        title="Alert ingestion token"
        description="A separate credential from your admin key: a monitoring system can open incidents but cannot read or change your API keys."
        actions={
          <button className="btn secondary small" type="button" onClick={rotate} disabled={busy}>
            {busy ? "Rotating..." : "Rotate token"}
          </button>
        }
      >
        {token ? (
          <>
            <div className="notice ok">
              New token issued. The previous one no longer works — update your monitoring
              integrations now. This is the only time it is shown.
            </div>
            <pre className="code">{token}</pre>
          </>
        ) : (
          <p style={{ fontSize: 13, color: "var(--text-muted)" }}>
            Tokens are stored only as digests and cannot be displayed again. Rotate to
            issue a new one if it has been lost.
          </p>
        )}
      </Card>

      <div className="grid two">
        <Card title="Grafana" description="Alerting → Contact points → Webhook.">
          <div className="field">
            <label>URL</label>
            <pre className="code">{endpoint("grafana")}</pre>
          </div>
          <div className="field">
            <label>HTTP header</label>
            <pre className="code">X-Criaisis-Token: &lt;alert token&gt;</pre>
          </div>
          <p style={{ fontSize: 12.5, color: "var(--text-muted)" }}>
            Severity is taken from the alert label; an unlabelled alert is treated as
            sev-2 rather than silently demoted.
          </p>
        </Card>

        <Card title="AWS CloudWatch" description="Point an SNS subscription at this endpoint.">
          <div className="field">
            <label>URL</label>
            <pre className="code">{endpoint("cloudwatch")}</pre>
          </div>
          <div className="field">
            <label>HTTP header</label>
            <pre className="code">X-Criaisis-Token: &lt;alert token&gt;</pre>
          </div>
          <p style={{ fontSize: 12.5, color: "var(--text-muted)" }}>
            The SNS subscription handshake is completed automatically. Only SubscribeURLs
            on amazonaws.com are followed.
          </p>
        </Card>
      </div>

      <Card title="Reaching this host">
        <p style={{ fontSize: 13, lineHeight: 1.65 }}>
          Grafana Cloud and AWS must be able to reach the endpoint above. For local
          development, expose it with a tunnel:
        </p>
        <pre className="code">cloudflared tunnel --url http://localhost:8088</pre>
      </Card>
    </Shell>
  );
}
