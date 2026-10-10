"use client";

import type { Classification, PersonaKey, Severity } from "@/lib/types";

/** Severity drives the status palette; specialist identity drives the domain
 *  palette. They are kept separate so a colour never means two things. */
const SEVERITY_TONE: Record<Severity, string> = {
  "sev-1": "critical",
  "sev-2": "high",
  "sev-3": "medium",
  "sev-4": "muted",
};

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`badge ${SEVERITY_TONE[severity] ?? "muted"}`}>{severity.toUpperCase()}</span>;
}

export function PersonaBadge({ persona }: { persona: PersonaKey | string }) {
  return <span className={`badge ${persona}`}>{persona}</span>;
}

const CLASSIFICATION_LABEL: Record<Classification, string> = {
  code: "Code-level",
  infra: "Infrastructure",
  hybrid: "Hybrid",
};

export function ClassificationBadge({ value }: { value: Classification | string }) {
  const label = CLASSIFICATION_LABEL[value as Classification] ?? value;
  const tone = value === "code" ? "accent" : value === "infra" ? "high" : "medium";
  return <span className={`badge ${tone}`}>{label}</span>;
}

export function Card({ title, description, actions, children, tight }: {
  title?: string;
  description?: string;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  tight?: boolean;
}) {
  return (
    <section className="card">
      {title ? (
        <div className="card-head">
          <div>
            <h2>{title}</h2>
            {description ? <p>{description}</p> : null}
          </div>
          {actions ? <div className="row">{actions}</div> : null}
        </div>
      ) : null}
      {children ? <div className={`card-body${tight ? " tight" : ""}`}>{children}</div> : null}
    </section>
  );
}

export function Stat({ label, value, note, mono }: {
  label: string;
  value: string | number;
  note?: string;
  mono?: boolean;
}) {
  return (
    <div className="card stat">
      <span className="stat-label">{label}</span>
      <span className={`stat-value${mono ? " mono" : ""}`}>{value}</span>
      {note ? <span className="stat-note">{note}</span> : null}
    </div>
  );
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function Check({ done, label, sub }: { done: boolean; label: string; sub?: string }) {
  return (
    <div className="check">
      <span className={`check-dot ${done ? "done" : "todo"}`}>{done ? "✓" : ""}</span>
      <span className="check-label">
        {label}
        {sub ? <span className="sub"> — {sub}</span> : null}
      </span>
    </div>
  );
}

/** formatTime renders a timestamp in the reader's locale without pretending to
 *  a precision the list view needs. */
export function formatTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString(undefined, {
    month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  });
}
