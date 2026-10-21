import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import OverviewPage from "@/app/page";
import SetupPage from "@/app/setup/page";
import RunbooksPage from "@/app/runbooks/page";
import SpecialistsPage from "@/app/specialists/page";
import IntegrationsPage from "@/app/integrations/page";
import IncidentsPage from "@/app/incidents/page";
import IncidentPage from "@/app/incidents/[id]/page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "test-incident-uuid" }),
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/incidents/test-incident-uuid",
}));

vi.mock("@/lib/session", () => ({
  useSession: () => ({
    creds: { workspace: "acme", adminKey: "admin-secret-key" },
    ready: true,
    signIn: vi.fn(),
    signOut: vi.fn(),
  }),
  useCredentials: () => ({ workspace: "acme", adminKey: "admin-secret-key" }),
  SessionProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/theme", () => ({
  useTheme: () => ({
    theme: "dark",
    toggleTheme: vi.fn(),
  }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/api", () => ({
  api: {
    settings: vi.fn().mockResolvedValue({
      team_id: "acme",
      name: "Acme Corp",
      llm: { provider: "anthropic", api_key_set: true, specialist_model: "claude-3-5-sonnet", synthesis_model: "claude-3-5-sonnet" },
      embeddings: { provider: "openai", api_key_set: true, model: "text-embedding-3-small", base_url: "" },
      notifications: { provider: "slack", webhook_set: true },
      ready_to_investigate: true,
      missing: null,
    }),
    incidents: vi.fn().mockResolvedValue({
      incidents: [
        {
          id: "test-incident-uuid",
          title: "Database connection pool exhausted",
          description: "Spike in connection pool wait times",
          severity: "sev-1",
          status: "resolved",
          trigger: "alert",
          created_at: new Date().toISOString(),
        },
      ],
    }),
    incident: vi.fn().mockResolvedValue({
      id: "test-incident-uuid",
      title: "Database connection pool exhausted",
      description: "Spike in connection pool wait times",
      severity: "sev-1",
      status: "resolved",
      trigger: "alert",
      created_at: new Date().toISOString(),
      transcript: [],
    }),
    runbooks: vi.fn().mockResolvedValue({ coverage: [] }),
    specialists: vi.fn().mockResolvedValue({
      specialists: [
        { key: "database", display_name: "Database Specialist", system_prompt: "prompt", enabled: true, runbooks: 1, chunks: 5 },
      ],
    }),
    testInvestigation: vi.fn().mockResolvedValue({}),
    rotateAlertToken: vi.fn().mockResolvedValue({ alert_token: "token", note: "rotated" }),
    uploadRunbook: vi.fn().mockResolvedValue({ status: "ok", title: "DB Runbook", chunks: 3 }),
    triggerSandbox: vi.fn().mockResolvedValue({ id: "repro-1", status: "Provisioning" }),
    getSandbox: vi.fn().mockResolvedValue({ id: "repro-1", status: "Ready" }),
    getSandboxAccess: vi.fn().mockResolvedValue({ access_url: "http://localhost:9000", logs_url: "http://localhost:9000/logs" }),
    selectSandboxScenario: vi.fn().mockResolvedValue({ id: "repro-1", status: "Provisioning" }),
  },
}));

describe("Web Dashboard Smoke Tests (US6.1, AC6.1.1, AC6.1.2)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("Page 1: Overview page renders without throwing errors", () => {
    render(<OverviewPage />);
    expect(screen.getByRole("heading", { name: "Overview" })).toBeInTheDocument();
  });

  it("Page 2: Setup page renders credentials and configuration sections", () => {
    render(<SetupPage />);
    expect(screen.getByRole("heading", { name: "Setup" })).toBeInTheDocument();
  });

  it("Page 3: Runbooks page renders coverage and upload form", () => {
    render(<RunbooksPage />);
    expect(screen.getByRole("heading", { name: "Runbooks" })).toBeInTheDocument();
  });

  it("Page 4: Specialists page renders persona list", () => {
    render(<SpecialistsPage />);
    expect(screen.getByRole("heading", { name: "Specialists" })).toBeInTheDocument();
  });

  it("Page 5: Integrations page renders alert token and webhook settings", () => {
    render(<IntegrationsPage />);
    expect(screen.getByRole("heading", { name: "Integrations" })).toBeInTheDocument();
  });

  it("Page 6: Incidents queue page renders incident history", () => {
    render(<IncidentsPage />);
    expect(screen.getByRole("heading", { name: "Incidents" })).toBeInTheDocument();
  });

  it("Page 7: Incident detail page renders header and SandboxReproductionPanel", async () => {
    render(<IncidentPage />);
    expect(screen.getByRole("heading", { name: "Incident" })).toBeInTheDocument();
    expect(await screen.findByText(/Database connection pool exhausted/i)).toBeInTheDocument();
  });
});
