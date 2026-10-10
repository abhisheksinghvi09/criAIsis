package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
)

// sampleIncidentTitle is deliberately explicit, so a test investigation is never
// mistaken for a real outage by whoever is watching the channel.
const sampleIncidentTitle = "criAIsis setup check: simulated checkout failure"

// RunTestInvestigation runs a complete, real investigation on a representative
// incident using the tenant's own credentials and their own runbooks, and delivers
// it to their channel.
//
// This is the onboarding proof: it exercises the model key, the embedding key,
// retrieval over whatever runbooks they have uploaded, and delivery. Anything
// broken surfaces here rather than during a real outage.
func (h *WorkspaceHandler) RunTestInvestigation(w http.ResponseWriter, r *http.Request) {
	ws, settings, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if !settings.IsReadyToInvestigate() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "this workspace is not ready to investigate",
			"missing": settings.MissingRequirements(),
		})
		return
	}

	inc, err := sampleIncident(ws.ID())
	if err != nil {
		h.fail(w, "building sample incident", err)
		return
	}

	// The incident is persisted like any other, so the transcript is auditable and
	// the customer can see exactly what their keys produced.
	result, err := h.incidents.InvestigateNow(r.Context(), inc)
	if err != nil {
		h.log.Error().Err(err).Str("workspace", ws.SlackTeamID()).Msg("setup check failed")
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "investigation complete and delivered",
		"incident_id":    inc.ID().String(),
		"specialists":    result.Specialists,
		"classification": result.Classification,
		"owning_domain":  result.OwningDomain,
		"elapsed":        result.Elapsed,
		"runbooks_cited": result.Citations,
		"note":           "Check your Slack or Discord channel: the full transcript was posted there.",
	})
}

// sampleIncident is a realistic, clearly-labelled incident with the kind of
// telemetry a real alert carries, so retrieval has something concrete to match.
func sampleIncident(wsID value.WorkspaceID) (*entity.Incident, error) {
	channel, err := value.NewSlackChannelID("C_SETUP_CHECK")
	if err != nil {
		return nil, err
	}
	thread, err := value.NewSlackThreadTS(time.Now().UTC().Format("20060102150405") + ".000000")
	if err != nil {
		return nil, err
	}
	user, err := value.NewSlackUserID("U_SETUP_CHECK")
	if err != nil {
		return nil, err
	}

	inc, err := entity.NewIncident(
		wsID, sampleIncidentTitle,
		"Checkout API returning HTTP 504 and the database is refusing new connections. "+
			"This is a setup check run from the criAIsis management API, not a real outage.",
		channel, thread, value.SeveritySev3, user, json.RawMessage("{}"),
	)
	if err != nil {
		return nil, err
	}
	if err := inc.SetTriggerType(value.TriggerTypeSlashCommand); err != nil {
		return nil, err
	}

	err = inc.SetIncidentContext(value.NewIncidentContext(
		"manual", "criAIsis setup check",
		[]string{
			"FATAL: remaining connection slots are reserved for non-replication superuser connections",
			"pgx: failed to acquire connection from pool: timeout after 5000ms",
			"HTTP 504 POST /api/v1/checkout duration=5003ms",
		},
		[]string{"repository.(*OrderRepo).CreateOrderWithInventoryLock\n\t/app/internal/repository/order.go:88"},
		map[string]float64{
			"db_active_connections":        200,
			"db_max_connections":           200,
			"db_idle_in_transaction_count": 142,
			"http_5xx_rate_percent":        68.5,
		},
	))
	return inc, err
}
