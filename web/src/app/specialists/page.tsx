"use client";

import { useEffect, useState } from "react";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card, Empty, PersonaBadge } from "@/components/ui";
import { api } from "@/lib/api";
import { useCredentials } from "@/lib/session";
import { useResource } from "@/lib/useApi";
import type { Specialist } from "@/lib/types";

export default function SpecialistsPage() {
  return (
    <AuthGate>
      <Specialists />
    </AuthGate>
  );
}

function Specialists() {
  const specialists = useResource<{ specialists: Specialist[] }>(api.specialists);
  const rows = specialists.data?.specialists ?? [];

  return (
    <Shell title="Specialists" subtitle="Four fixed domains. Prompts are yours to tune.">
      {specialists.error ? <div className="notice error">{specialists.error}</div> : null}

      <div className="notice info">
        The read-only mandate is prepended by the engine to every prompt below and is
        not editable here, so tuning a specialist cannot remove the safety boundary.
      </div>

      {specialists.loading ? (
        <Card><Empty>Loading specialists…</Empty></Card>
      ) : (
        rows.map((specialist) => (
          <SpecialistCard key={specialist.key} specialist={specialist} onSaved={specialists.reload} />
        ))
      )}
    </Shell>
  );
}

function SpecialistCard({ specialist, onSaved }: { specialist: Specialist; onSaved: () => void }) {
  const creds = useCredentials();
  const [prompt, setPrompt] = useState(specialist.system_prompt);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ tone: "ok" | "error"; text: string } | null>(null);

  // Keep the editor in step when the list is revalidated after a save elsewhere.
  useEffect(() => setPrompt(specialist.system_prompt), [specialist.system_prompt]);

  const dirty = prompt !== specialist.system_prompt;

  async function save(body: { system_prompt?: string; enabled?: boolean }) {
    setBusy(true);
    setMessage(null);
    try {
      await api.updateSpecialist(creds, specialist.key, body);
      setMessage({ tone: "ok", text: "Saved." });
      onSaved();
    } catch (err) {
      setMessage({ tone: "error", text: err instanceof Error ? err.message : "Save failed" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      title={specialist.display_name}
      description={`${specialist.runbooks} runbook${specialist.runbooks === 1 ? "" : "s"} · ${specialist.chunks} chunks indexed`}
      actions={
        <>
          <PersonaBadge persona={specialist.key} />
          <span className={`badge ${specialist.enabled ? "ok" : "muted"}`}>
            {specialist.enabled ? "Participating" : "Disabled"}
          </span>
          <button
            className="btn secondary small"
            type="button"
            disabled={busy}
            onClick={() => save({ enabled: !specialist.enabled })}
          >
            {specialist.enabled ? "Disable" : "Enable"}
          </button>
        </>
      }
    >
      {message ? <div className={`notice ${message.tone}`}>{message.text}</div> : null}
      {specialist.runbooks === 0 ? (
        <div className="notice warn">
          No runbooks. This specialist will report no confidence rather than guessing.
        </div>
      ) : null}

      <div className="field">
        <label htmlFor={`prompt-${specialist.key}`}>System prompt</label>
        <textarea
          id={`prompt-${specialist.key}`}
          className="textarea"
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={7}
        />
      </div>

      <div className="row end">
        {dirty ? (
          <button className="btn subtle small" type="button" onClick={() => setPrompt(specialist.system_prompt)}>
            Discard
          </button>
        ) : null}
        <button className="btn primary" type="button" disabled={busy || !dirty} onClick={() => save({ system_prompt: prompt })}>
          {busy ? "Saving..." : "Save prompt"}
        </button>
      </div>
    </Card>
  );
}
