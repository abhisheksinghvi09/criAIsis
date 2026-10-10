/** Shapes returned by the criAIsis Go API. Kept in one place so a route change
 *  breaks the type check rather than the running dashboard. */

export type PersonaKey = "network" | "database" | "application" | "security";
export type NotifyProvider = "none" | "slack" | "discord";
export type Severity = "sev-1" | "sev-2" | "sev-3" | "sev-4";
export type Classification = "code" | "infra" | "hybrid";

export interface WorkspaceSettings {
  team_id: string;
  name: string;
  llm: {
    provider: string;
    /** The key itself is never returned by the API, only whether one is stored. */
    api_key_set: boolean;
    specialist_model: string;
    synthesis_model: string;
  };
  embeddings: {
    provider: string;
    api_key_set: boolean;
    model: string;
    base_url: string;
  };
  notifications: {
    provider: NotifyProvider;
    webhook_set: boolean;
  };
  ready_to_investigate: boolean;
  missing: string[] | null;
}

export interface Specialist {
  key: PersonaKey;
  display_name: string;
  system_prompt: string;
  enabled: boolean;
  runbooks: number;
  chunks: number;
}

export interface RunbookCoverage {
  persona: PersonaKey;
  enabled: boolean;
  runbooks: number;
  chunks: number;
  titles: string[];
}

export interface IncidentContext {
  provider: string;
  alert_name: string;
  error_logs: string[] | null;
  stack_traces: string[] | null;
  metrics: Record<string, number> | null;
}

export interface IncidentSummary {
  id: string;
  title: string;
  description: string;
  severity: Severity;
  status: string;
  trigger: string;
  created_at: string;
  resolved_at?: string;
  context?: IncidentContext;
}

/** The JSONB envelope persisted alongside every debate turn. */
export interface TurnMetadata {
  citations?: Citation[];
  confidence?: string;
  classification?: Classification;
  owning_domain?: PersonaKey;
  contradictions?: string[];
  next_steps?: string[];
  diagnostic_queries?: string[];
}

export interface Citation {
  chunk_id: string;
  document_title: string;
  snippet: string;
  source?: string;
}

export interface DebateTurn {
  id: string;
  stage: 1 | 2 | 3;
  turn_type: "specialist_hypothesis" | "synthesis" | "follow_up";
  content: string;
  created_at: string;
  persona?: string;
  citations?: Citation[] | null;
  metadata: TurnMetadata;
}

export interface IncidentDetail extends IncidentSummary {
  transcript: DebateTurn[];
}

export interface TestInvestigationResult {
  status: string;
  incident_id: string;
  specialists: number;
  classification: Classification;
  owning_domain: PersonaKey;
  elapsed: string;
  runbooks_cited: string[] | null;
  note: string;
}

export interface SaveResult {
  status: string;
  ready_to_investigate: boolean;
  missing: string[] | null;
}

export interface Credentials {
  workspace: string;
  adminKey: string;
}

export type SandboxStatus = "Provisioning" | "Ready" | "UnmatchedFallback" | "Failed";

export interface SandboxReproduction {
  id: string;
  incident_id: string;
  status: SandboxStatus;
  scenario_id?: string | null;
  container_ref?: string | null;
  created_at: string;
  ready_at?: string | null;
}

export interface SandboxAccess {
  container_ref: string;
  access_url: string;
  logs_url: string;
}
