import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { SandboxReproductionPanel } from "@/components/SandboxReproductionPanel";
import { api } from "@/lib/api";
import type { Credentials, SandboxAccess, SandboxReproduction } from "@/lib/types";

vi.mock("@/lib/api", () => ({
  api: {
    triggerSandbox: vi.fn(),
    getSandbox: vi.fn(),
    getSandboxAccess: vi.fn(),
    selectSandboxScenario: vi.fn(),
  },
}));

const mockCreds: Credentials = {
  workspace: "test-workspace",
  adminKey: "test-key-12345",
};

describe("SandboxReproductionPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders notice when incident is not resolved", () => {
    render(
      <SandboxReproductionPanel
        incidentId="inc-123"
        incidentStatus="investigating"
        credentials={mockCreds}
      />
    );

    expect(
      screen.getByText(/Sandbox reproduction is available once an incident is marked resolved/i)
    ).toBeInTheDocument();
    expect(screen.queryByTestId("reproduce-sandbox-btn")).not.toBeInTheDocument();
  });

  it("renders idle state with trigger button for resolved incident", () => {
    render(
      <SandboxReproductionPanel
        incidentId="inc-123"
        incidentStatus="resolved"
        credentials={mockCreds}
      />
    );

    const triggerBtn = screen.getByTestId("reproduce-sandbox-btn");
    expect(triggerBtn).toBeInTheDocument();
    expect(triggerBtn).toHaveTextContent("Reproduce in Sandbox");
  });

  it("transitions to provisioning on trigger click and polls status to Ready", async () => {
    const reproProvisioning: SandboxReproduction = {
      id: "repro-456",
      incident_id: "inc-123",
      status: "Provisioning",
      created_at: new Date().toISOString(),
    };

    const reproReady: SandboxReproduction = {
      id: "repro-456",
      incident_id: "inc-123",
      status: "Ready",
      scenario_id: "oom_crashloop",
      container_ref: "criaisis-repro-container-1",
      created_at: new Date().toISOString(),
      ready_at: new Date().toISOString(),
    };

    const accessInfo: SandboxAccess = {
      container_ref: "criaisis-repro-container-1",
      access_url: "http://localhost:9000/connect",
      logs_url: "http://localhost:9000/logs",
    };

    vi.mocked(api.triggerSandbox).mockResolvedValueOnce(reproProvisioning);
    vi.mocked(api.getSandbox).mockResolvedValue(reproReady);
    vi.mocked(api.getSandboxAccess).mockResolvedValueOnce(accessInfo);

    render(
      <SandboxReproductionPanel
        incidentId="inc-123"
        incidentStatus="resolved"
        credentials={mockCreds}
      />
    );

    const triggerBtn = screen.getByTestId("reproduce-sandbox-btn");
    fireEvent.click(triggerBtn);

    await waitFor(() => {
      expect(api.triggerSandbox).toHaveBeenCalledWith(mockCreds, "inc-123");
    });

    await waitFor(() => {
      expect(screen.getByTestId("sandbox-ready-notice")).toBeInTheDocument();
    });

    expect(screen.getByTestId("open-sandbox-btn")).toHaveAttribute("href", "http://localhost:9000/connect");
    expect(screen.getByTestId("view-logs-btn")).toHaveAttribute("href", "http://localhost:9000/logs");
  });

  it("handles unmatched fallback scenario selection", async () => {
    const reproFallback: SandboxReproduction = {
      id: "repro-789",
      incident_id: "inc-123",
      status: "UnmatchedFallback",
      created_at: new Date().toISOString(),
    };

    const reproProvisioning: SandboxReproduction = {
      id: "repro-789",
      incident_id: "inc-123",
      status: "Provisioning",
      created_at: new Date().toISOString(),
    };

    vi.mocked(api.triggerSandbox).mockResolvedValueOnce(reproFallback);
    vi.mocked(api.selectSandboxScenario).mockResolvedValueOnce(reproProvisioning);
    vi.mocked(api.getSandbox).mockResolvedValue(reproProvisioning);

    render(
      <SandboxReproductionPanel
        incidentId="inc-123"
        incidentStatus="resolved"
        credentials={mockCreds}
      />
    );

    fireEvent.click(screen.getByTestId("reproduce-sandbox-btn"));

    await waitFor(() => {
      expect(screen.getByTestId("sandbox-fallback-notice")).toBeInTheDocument();
    });

    const select = screen.getByTestId("sandbox-scenario-select");
    fireEvent.change(select, { target: { value: "db_connection_exhaustion" } });

    const startBtn = screen.getByTestId("start-scenario-btn");
    fireEvent.click(startBtn);

    await waitFor(() => {
      expect(api.selectSandboxScenario).toHaveBeenCalledWith(
        mockCreds,
        "inc-123",
        "repro-789",
        "db_connection_exhaustion"
      );
      expect(screen.getByTestId("sandbox-provisioning-notice")).toBeInTheDocument();
    });
  });

  it("displays error banner when trigger fails", async () => {
    vi.mocked(api.triggerSandbox).mockRejectedValueOnce(new Error("Network timeout reaching server"));

    render(
      <SandboxReproductionPanel
        incidentId="inc-123"
        incidentStatus="resolved"
        credentials={mockCreds}
      />
    );

    fireEvent.click(screen.getByTestId("reproduce-sandbox-btn"));

    await waitFor(() => {
      expect(screen.getByTestId("sandbox-error-notice")).toBeInTheDocument();
      expect(screen.getByText(/Network timeout reaching server/i)).toBeInTheDocument();
    });
  });
});
