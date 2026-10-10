"use client";

import { useState } from "react";
import { AuthGate } from "@/components/AuthGate";
import { Shell } from "@/components/Shell";
import { Card, Empty, PersonaBadge } from "@/components/ui";
import { api } from "@/lib/api";
import { useCredentials } from "@/lib/session";
import { useResource } from "@/lib/useApi";
import type { PersonaKey, RunbookCoverage } from "@/lib/types";

const PERSONAS: PersonaKey[] = ["network", "database", "application", "security"];

export default function RunbooksPage() {
  return (
    <AuthGate>
      <Runbooks />
    </AuthGate>
  );
}

function Runbooks() {
  const creds = useCredentials();
  const coverage = useResource<{ coverage: RunbookCoverage[] }>(api.runbooks);

  const [persona, setPersona] = useState<PersonaKey>("database");
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ tone: "ok" | "error"; text: string } | null>(null);

  async function upload() {
    setBusy(true);
    setMessage(null);
    try {
      const result = await api.uploadRunbook(creds, { persona, title: title.trim(), content });
      setMessage({ tone: "ok", text: `${result.status}: ${result.title} (${result.chunks} chunks)` });
      setTitle("");
      setContent("");
      coverage.reload();
    } catch (err) {
      setMessage({ tone: "error", text: err instanceof Error ? err.message : "Upload failed" });
    } finally {
      setBusy(false);
    }
  }

  async function readFile(file: File) {
    setContent(await file.text());
    if (!title.trim()) setTitle(file.name.replace(/\.(md|markdown|txt)$/i, ""));
  }

  const rows = coverage.data?.coverage ?? [];
  const empty = rows.every((row) => row.runbooks === 0);

  return (
    <Shell title="Runbooks" subtitle="What each specialist is allowed to cite">
      {coverage.error ? <div className="notice error">{coverage.error}</div> : null}
      {empty && !coverage.loading ? (
        <div className="notice warn">
          No runbooks loaded. Specialists will correctly report no confidence during an
          incident rather than inventing advice.
        </div>
      ) : null}

      <Card title="Coverage" description="Retrieval is scoped to one specialist at a time, so a persona can only cite its own documents.">
        {coverage.loading ? (
          <Empty>Loading coverage…</Empty>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Specialist</th>
                  <th>Runbooks</th>
                  <th>Chunks</th>
                  <th>Documents</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.persona}>
                    <td><PersonaBadge persona={row.persona} /></td>
                    <td className="mono">{row.runbooks}</td>
                    <td className="mono">{row.chunks}</td>
                    <td>
                      {row.titles.length === 0 ? (
                        <span style={{ color: "var(--text-muted)" }}>none</span>
                      ) : (
                        row.titles.join(", ")
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card title="Add a runbook" description="Chunked and embedded on your own key. Re-uploading unchanged content is skipped.">
        {message ? <div className={`notice ${message.tone}`}>{message.text}</div> : null}

        <div className="row">
          <div className="field" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rb-persona">Specialist</label>
            <select id="rb-persona" className="select" value={persona} onChange={(e) => setPersona(e.target.value as PersonaKey)}>
              {PERSONAS.map((key) => (
                <option key={key} value={key}>{key}</option>
              ))}
            </select>
          </div>
          <div className="field" style={{ flex: 2, minWidth: 220 }}>
            <label htmlFor="rb-title">Title</label>
            <input id="rb-title" className="input" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="PostgreSQL Connection Saturation" />
          </div>
          <div className="field" style={{ flex: 1, minWidth: 180 }}>
            <label htmlFor="rb-file">Or load a file</label>
            <input
              id="rb-file"
              className="input"
              type="file"
              accept=".md,.markdown,.txt"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) void readFile(file);
              }}
            />
          </div>
        </div>

        <div className="field">
          <label htmlFor="rb-content">Markdown</label>
          <textarea
            id="rb-content"
            className="textarea mono"
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder={"# Runbook\n\n## Symptoms\n\n## Diagnostic queries\n\n```sql\nSELECT ...\n```"}
            rows={14}
          />
          <span className="hint">Code fences are kept whole when chunking, so a diagnostic query is never split in half.</span>
        </div>

        <div className="row end">
          <button className="btn primary" type="button" disabled={busy || !title.trim() || !content.trim()} onClick={upload}>
            {busy ? "Indexing..." : "Upload and index"}
          </button>
        </div>
      </Card>
    </Shell>
  );
}
