"use client";

import { useState } from "react";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card, Check } from "@/components/ui";
import { api } from "@/lib/api";
import { useCredentials } from "@/lib/session";
import { useResource } from "@/lib/useApi";
import type { WorkspaceSettings } from "@/lib/types";

export default function SetupPage() {
  return (
    <AuthGate>
      <Setup />
    </AuthGate>
  );
}

function Setup() {
  const settings = useResource<WorkspaceSettings>(api.settings);
  const s = settings.data;

  return (
    <Shell
      title="Setup"
      subtitle="Your own keys. criAIsis holds no shared model credential."
    >
      {settings.error ? <div className="notice error">{settings.error}</div> : null}

      <Card title="Readiness" description="Each credential is tested against the live provider before it is stored.">
        <div className="checklist">
          <Check done={s?.llm.api_key_set ?? false} label="Anthropic key" sub="runs the four specialists and the commander" />
          <Check done={s?.embeddings.api_key_set ?? false} label="Embeddings key" sub="indexes and retrieves your runbooks" />
          <Check done={(s?.notifications.provider ?? "none") !== "none"} label="Slack or Discord webhook" sub="where finished investigations are posted" />
        </div>
      </Card>

      <div className="grid two">
        <LLMForm settings={s} onSaved={settings.reload} />
        <EmbeddingsForm settings={s} onSaved={settings.reload} />
      </div>

      <NotificationsForm settings={s} onSaved={settings.reload} />
    </Shell>
  );
}

/** useSaver centralises the pending/ok/error cycle every form on this page shares. */
function useSaver() {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ tone: "ok" | "error"; text: string } | null>(null);

  async function run(action: () => Promise<string>) {
    setBusy(true);
    setMessage(null);
    try {
      setMessage({ tone: "ok", text: await action() });
    } catch (err) {
      setMessage({ tone: "error", text: err instanceof Error ? err.message : "Request failed" });
    } finally {
      setBusy(false);
    }
  }

  return { busy, message, run };
}

function Notice({ message }: { message: { tone: "ok" | "error"; text: string } | null }) {
  if (!message) return null;
  return <div className={`notice ${message.tone}`}>{message.text}</div>;
}

function LLMForm({ settings, onSaved }: { settings: WorkspaceSettings | null; onSaved: () => void }) {
  const creds = useCredentials();
  const { busy, message, run } = useSaver();
  const [apiKey, setApiKey] = useState("");
  const [specialist, setSpecialist] = useState("");
  const [synthesis, setSynthesis] = useState("");

  return (
    <Card
      title="Anthropic"
      description="Powers Stage 1 specialist hypotheses and the Stage 2 commander verdict."
      actions={settings?.llm.api_key_set ? <span className="badge ok">Configured</span> : <span className="badge muted">Not set</span>}
    >
      <Notice message={message} />

      <div className="field">
        <label htmlFor="llm-key">API key</label>
        <input
          id="llm-key"
          className="input mono"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={settings?.llm.api_key_set ? "stored — enter a new key to replace it" : "sk-ant-..."}
          autoComplete="off"
        />
        <span className="hint">Encrypted at rest. It is verified with a live call before being saved.</span>
      </div>

      <div className="row">
        <div className="field" style={{ flex: 1, minWidth: 150 }}>
          <label htmlFor="llm-specialist">Specialist model</label>
          <input
            id="llm-specialist"
            className="input mono"
            value={specialist}
            onChange={(e) => setSpecialist(e.target.value)}
            placeholder={settings?.llm.specialist_model ?? "claude-opus-5"}
          />
        </div>
        <div className="field" style={{ flex: 1, minWidth: 150 }}>
          <label htmlFor="llm-synthesis">Synthesis model</label>
          <input
            id="llm-synthesis"
            className="input mono"
            value={synthesis}
            onChange={(e) => setSynthesis(e.target.value)}
            placeholder={settings?.llm.synthesis_model ?? "claude-opus-5"}
          />
        </div>
      </div>

      <div className="row end">
        <button
          className="btn primary"
          type="button"
          disabled={busy || apiKey.trim() === ""}
          onClick={() =>
            run(async () => {
              const result = await api.setLLM(creds, {
                api_key: apiKey.trim(),
                ...(specialist.trim() ? { specialist_model: specialist.trim() } : {}),
                ...(synthesis.trim() ? { synthesis_model: synthesis.trim() } : {}),
              });
              setApiKey("");
              onSaved();
              return result.status;
            })
          }
        >
          {busy ? "Verifying..." : "Verify and save"}
        </button>
      </div>
    </Card>
  );
}

function EmbeddingsForm({ settings, onSaved }: { settings: WorkspaceSettings | null; onSaved: () => void }) {
  const creds = useCredentials();
  const { busy, message, run } = useSaver();
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("");
  const [baseURL, setBaseURL] = useState("");

  return (
    <Card
      title="Embeddings"
      description="Anthropic exposes no embeddings endpoint, so retrieval uses a separate OpenAI-compatible key."
      actions={settings?.embeddings.api_key_set ? <span className="badge ok">Configured</span> : <span className="badge muted">Not set</span>}
    >
      <Notice message={message} />

      <div className="field">
        <label htmlFor="emb-key">API key</label>
        <input
          id="emb-key"
          className="input mono"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={settings?.embeddings.api_key_set ? "stored — enter a new key to replace it" : "sk-..."}
          autoComplete="off"
        />
        <span className="hint">Must return 1536 dimensions; this is checked before saving.</span>
      </div>

      <div className="row">
        <div className="field" style={{ flex: 1, minWidth: 150 }}>
          <label htmlFor="emb-model">Model</label>
          <input
            id="emb-model"
            className="input mono"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder={settings?.embeddings.model ?? "text-embedding-3-small"}
          />
        </div>
        <div className="field" style={{ flex: 1, minWidth: 180 }}>
          <label htmlFor="emb-url">Base URL</label>
          <input
            id="emb-url"
            className="input mono"
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
            placeholder={settings?.embeddings.base_url ?? "https://api.openai.com/v1"}
          />
        </div>
      </div>

      <div className="row end">
        <button
          className="btn primary"
          type="button"
          disabled={busy || apiKey.trim() === ""}
          onClick={() =>
            run(async () => {
              const result = await api.setEmbeddings(creds, {
                api_key: apiKey.trim(),
                ...(model.trim() ? { model: model.trim() } : {}),
                ...(baseURL.trim() ? { base_url: baseURL.trim() } : {}),
              });
              setApiKey("");
              onSaved();
              return result.status;
            })
          }
        >
          {busy ? "Verifying..." : "Verify and save"}
        </button>
      </div>
    </Card>
  );
}

function NotificationsForm({ settings, onSaved }: { settings: WorkspaceSettings | null; onSaved: () => void }) {
  const creds = useCredentials();
  const { busy, message, run } = useSaver();
  const [provider, setProvider] = useState<string>("");
  const [webhookURL, setWebhookURL] = useState("");

  const active = provider || settings?.notifications.provider || "none";

  return (
    <Card
      title="Where investigations are posted"
      description="An incoming webhook: no OAuth, no scopes, no app review."
      actions={
        settings?.notifications.webhook_set ? (
          <button
            className="btn secondary small"
            type="button"
            disabled={busy}
            onClick={() => run(async () => (await api.testNotification(creds)).status)}
          >
            Send test message
          </button>
        ) : null
      }
    >
      <Notice message={message} />

      <div className="row">
        <div className="field" style={{ flex: 1, minWidth: 160 }}>
          <label htmlFor="notify-provider">Destination</label>
          <select
            id="notify-provider"
            className="select"
            value={active}
            onChange={(e) => setProvider(e.target.value)}
          >
            <option value="none">None</option>
            <option value="slack">Slack</option>
            <option value="discord">Discord</option>
          </select>
        </div>
        <div className="field" style={{ flex: 3, minWidth: 260 }}>
          <label htmlFor="notify-url">Webhook URL</label>
          <input
            id="notify-url"
            className="input mono"
            type="password"
            value={webhookURL}
            onChange={(e) => setWebhookURL(e.target.value)}
            placeholder={
              settings?.notifications.webhook_set
                ? "stored — enter a new url to replace it"
                : active === "discord"
                  ? "https://discord.com/api/webhooks/..."
                  : "https://hooks.slack.com/services/..."
            }
            autoComplete="off"
            disabled={active === "none"}
          />
          <span className="hint">Must be https and on the provider&apos;s own domain. Encrypted at rest.</span>
        </div>
      </div>

      <div className="row end">
        <button
          className="btn primary"
          type="button"
          disabled={busy || (active !== "none" && webhookURL.trim() === "")}
          onClick={() =>
            run(async () => {
              const result = await api.setNotifications(creds, {
                provider: active,
                ...(webhookURL.trim() ? { webhook_url: webhookURL.trim() } : {}),
              });
              setWebhookURL("");
              onSaved();
              return result.status;
            })
          }
        >
          {busy ? "Saving..." : "Save destination"}
        </button>
      </div>
    </Card>
  );
}
