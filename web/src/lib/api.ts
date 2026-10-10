import type {
  Credentials,
  IncidentDetail,
  IncidentSummary,
  RunbookCoverage,
  SandboxAccess,
  SandboxReproduction,
  SaveResult,
  Specialist,
  TestInvestigationResult,
  WorkspaceSettings,
} from "./types";

/** Requests go through the Next rewrite to the Go API, so the admin key travels
 *  same-origin rather than as a cross-origin custom header. */
const BASE = "/api/v1/workspaces";

/** ApiError carries the server's own message, which is usually actionable
 *  ("the model credential was rejected: ..."), rather than a generic failure. */
export class ApiError extends Error {
  readonly status: number;
  readonly missing?: string[];

  constructor(status: number, message: string, missing?: string[]) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.missing = missing;
  }
}

async function request<T>(
  creds: Credentials,
  path: string,
  init: RequestInit = {},
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${BASE}/${encodeURIComponent(creds.workspace)}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${creds.adminKey}`,
        ...(init.headers ?? {}),
      },
    });
  } catch {
    throw new ApiError(0, "Could not reach the criAIsis API. Check that the server is running.");
  }

  const text = await response.text();

  // A proxy error, a crashed API or a gateway timeout returns HTML or plain
  // text. Parsing it blindly surfaces "Unexpected token '<'" to the operator,
  // which says nothing about what actually went wrong.
  let payload: unknown = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      throw new ApiError(
        response.status,
        response.ok
          ? "The API returned a response this dashboard could not read."
          : `The API is not reachable (status ${response.status}). Is the criAIsis server running?`,
      );
    }
  }

  if (!response.ok) {
    const body = payload as { error?: string; missing?: string[] };
    throw new ApiError(
      response.status,
      body.error ?? `Request failed with status ${response.status}`,
      body.missing,
    );
  }
  return payload as T;
}

export const api = {
  /** Also serves as the credential check on sign in.
   *  No trailing slash: Next normalises one away with a 308, costing a redirect
   *  round trip on the most frequently called endpoint. */
  settings: (c: Credentials) => request<WorkspaceSettings>(c, ""),

  setLLM: (c: Credentials, body: { api_key: string; specialist_model?: string; synthesis_model?: string }) =>
    request<SaveResult>(c, "/llm", { method: "PUT", body: JSON.stringify(body) }),

  setEmbeddings: (c: Credentials, body: { api_key: string; model?: string; base_url?: string }) =>
    request<SaveResult>(c, "/embeddings", { method: "PUT", body: JSON.stringify(body) }),

  setNotifications: (c: Credentials, body: { provider: string; webhook_url?: string }) =>
    request<SaveResult>(c, "/notifications", { method: "PUT", body: JSON.stringify(body) }),

  testNotification: (c: Credentials) =>
    request<{ status: string }>(c, "/notifications/test", { method: "POST" }),

  testInvestigation: (c: Credentials) =>
    request<TestInvestigationResult>(c, "/test-investigation", { method: "POST" }),

  rotateAlertToken: (c: Credentials) =>
    request<{ alert_token: string; note: string }>(c, "/alert-token/rotate", { method: "POST" }),

  runbooks: (c: Credentials) =>
    request<{ coverage: RunbookCoverage[] }>(c, "/runbooks"),

  uploadRunbook: (c: Credentials, body: { persona: string; title: string; content: string }) =>
    request<{ status: string; title: string; chunks: number }>(c, "/runbooks", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  incidents: (c: Credentials) =>
    request<{ incidents: IncidentSummary[] | null }>(c, "/incidents"),

  incident: (c: Credentials, id: string) =>
    request<IncidentDetail>(c, `/incidents/${id}`),

  specialists: (c: Credentials) =>
    request<{ specialists: Specialist[] }>(c, "/specialists"),

  updateSpecialist: (c: Credentials, key: string, body: { system_prompt?: string; enabled?: boolean }) =>
    request<Specialist>(c, `/specialists/${key}`, { method: "PATCH", body: JSON.stringify(body) }),

  triggerSandbox: (c: Credentials, incidentId: string) =>
    request<SandboxReproduction>(c, `/incidents/${incidentId}/sandbox`, { method: "POST" }),

  getSandbox: (c: Credentials, incidentId: string, reproId: string) =>
    request<SandboxReproduction>(c, `/incidents/${incidentId}/sandbox/${reproId}`),

  getSandboxAccess: (c: Credentials, incidentId: string, reproId: string) =>
    request<SandboxAccess>(c, `/incidents/${incidentId}/sandbox/${reproId}/access`),

  selectSandboxScenario: (c: Credentials, incidentId: string, reproId: string, scenarioId: string) =>
    request<SandboxReproduction>(c, `/incidents/${incidentId}/sandbox/${reproId}/select-scenario`, {
      method: "POST",
      body: JSON.stringify({ scenario_id: scenarioId }),
    }),
};
