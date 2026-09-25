package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// maxNoteLength bounds the free text an operator attaches to an action.
const maxNoteLength = 500

// maxMeterIDLength bounds the meter_id filter; real codes are far shorter.
const maxMeterIDLength = 32

// AnomalyStore reads anomalies and applies actions to them. Lookups return
// domain.ErrNotFound when nothing matches, and ApplyAction returns
// domain.ErrInvalidTransition when the action is not allowed in the current status.
type AnomalyStore interface {
	Anomalies(ctx context.Context, f domain.AnomalyFilter) ([]domain.Anomaly, error)
	Anomaly(ctx context.Context, id string) (domain.Anomaly, error)
	AnomalyActions(ctx context.Context, id string) ([]domain.AnomalyAction, error)
	ApplyAction(ctx context.Context, anomalyID, userID string, action domain.Action, note string) (domain.Anomaly, error)
}

type anomalyResource struct {
	store AnomalyStore
	log   *slog.Logger
}

func (a anomalyResource) routes(r chi.Router) {
	r.Get("/anomalies", a.list)
	r.Get("/anomalies/{id}", a.get)
	r.Post("/anomalies/{id}/actions", a.act)
}

// anomalyResponse is an anomaly as the API shows it. The first seven fields
// are the ones the challenge asks for, under its own names.
type anomalyResponse struct {
	MeterID           string  `json:"meter_id"`
	Anomaly           string  `json:"anomaly"`
	Type              string  `json:"type"`
	Severity          string  `json:"severity"`
	Confidence        float64 `json:"confidence"`
	Reason            string  `json:"reason"`
	RecommendedAction string  `json:"recommended_action"`

	ID         string    `json:"id"`
	MeterName  string    `json:"meter_name"`
	Location   string    `json:"location"`
	Priority   int       `json:"priority"`
	Status     string    `json:"status"`
	Ongoing    bool      `json:"ongoing"`
	Start      time.Time `json:"episode_start"`
	End        time.Time `json:"episode_end"`
	DetectedAt time.Time `json:"detected_at"`
	// NextAction is the action the platform recommends, or null once the anomaly is no longer open.
	NextAction        *string  `json:"recommended_next_action"`
	AvailableActions  []string `json:"available_actions"`
	ExplanationSource string   `json:"explanation_source"`
	ExplanationModel  string   `json:"explanation_model"`
}

// anomalyDetail adds what the investigation screen needs.
type anomalyDetail struct {
	anomalyResponse
	InvestigationSteps  []string         `json:"investigation_steps"`
	Evidence            domain.Evidence  `json:"evidence"`
	ConfidenceBreakdown json.RawMessage  `json:"confidence_breakdown"`
	PriorityBreakdown   json.RawMessage  `json:"priority_breakdown"`
	Actions             []actionResponse `json:"actions"`
}

type actionResponse struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	Note       string    `json:"note"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	UserName   string    `json:"user_name"`
	CreatedAt  time.Time `json:"created_at"`
}

func toAnomalyResponse(a domain.Anomaly) anomalyResponse {
	available := make([]string, 0)
	for _, act := range domain.AvailableActions(a.Status) {
		available = append(available, string(act))
	}
	var next *string
	if a.Status == domain.StatusOpen {
		n := string(domain.MainAction(a.Type))
		next = &n
	}
	return anomalyResponse{
		MeterID: a.MeterID, Anomaly: a.Explanation.Summary, Type: string(a.Type), Severity: string(a.Severity),
		Confidence: a.Confidence, Reason: a.Explanation.Reason, RecommendedAction: a.Explanation.RecommendedAction,
		ID: a.ID, MeterName: a.Evidence.MeterName, Location: a.Evidence.Location, Priority: a.Priority,
		Status: string(a.Status), Ongoing: a.Ongoing, Start: a.EpisodeStart.UTC(), End: a.EpisodeEnd.UTC(),
		DetectedAt: a.DetectedAt.UTC(), NextAction: next, AvailableActions: available,
		ExplanationSource: string(a.Explanation.Source), ExplanationModel: a.Explanation.Model,
	}
}

func toAnomalyDetail(a domain.Anomaly, history []domain.AnomalyAction) anomalyDetail {
	steps := a.Explanation.InvestigationSteps
	if steps == nil {
		steps = []string{}
	}
	actions := make([]actionResponse, len(history))
	for i, h := range history {
		actions[i] = actionResponse{
			ID: h.ID, Action: string(h.Action), Note: h.Note, FromStatus: string(h.From), ToStatus: string(h.To),
			UserName: h.UserName, CreatedAt: h.CreatedAt.UTC(),
		}
	}
	return anomalyDetail{
		anomalyResponse: toAnomalyResponse(a), InvestigationSteps: steps, Evidence: a.Evidence,
		ConfidenceBreakdown: a.ConfidenceBreakdown, PriorityBreakdown: a.PriorityBreakdown, Actions: actions,
	}
}

func (a anomalyResource) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.AnomalyFilter{
		Type:     domain.AnomalyType(q.Get("type")),
		Severity: domain.Severity(q.Get("severity")),
		Status:   domain.AnomalyStatus(q.Get("status")),
		MeterID:  q.Get("meter_id"),
	}
	if msg := validateFilter(filter); msg != "" {
		writeProblem(w, http.StatusBadRequest, msg)
		return
	}
	list, err := a.store.Anomalies(r.Context(), filter)
	if err != nil {
		serverError(w, a.log, "list anomalies", err)
		return
	}
	out := make([]anomalyResponse, len(list))
	for i, an := range list {
		out[i] = toAnomalyResponse(an)
	}
	writeJSON(w, http.StatusOK, out)
}

func validateFilter(f domain.AnomalyFilter) string {
	if f.Type != "" && !oneOf(f.Type, domain.RealAnomaly, domain.ExplainableAnomaly, domain.FalsePositive, domain.DataQuality) {
		return "type must be REAL_ANOMALY, EXPLAINABLE_ANOMALY, FALSE_POSITIVE or DATA_QUALITY"
	}
	if f.Severity != "" && !oneOf(f.Severity, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow) {
		return "severity must be HIGH, MEDIUM or LOW"
	}
	if f.Status != "" && !oneOf(f.Status, domain.StatusOpen, domain.StatusAcknowledged, domain.StatusResolved, domain.StatusDismissed) {
		return "status must be OPEN, ACKNOWLEDGED, RESOLVED or DISMISSED"
	}
	if utf8.RuneCountInString(f.MeterID) > maxMeterIDLength {
		return fmt.Sprintf("meter_id can have at most %d characters", maxMeterIDLength)
	}
	return ""
}

func oneOf[T comparable](v T, allowed ...T) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (a anomalyResource) get(w http.ResponseWriter, r *http.Request) {
	id, ok := anomalyID(w, r)
	if !ok {
		return
	}
	an, err := a.store.Anomaly(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "no anomaly has that id")
		return
	}
	if err != nil {
		serverError(w, a.log, "read anomaly", err)
		return
	}
	history, err := a.store.AnomalyActions(r.Context(), id)
	if err != nil {
		serverError(w, a.log, "read anomaly actions", err)
		return
	}
	writeJSON(w, http.StatusOK, toAnomalyDetail(an, history))
}

type actionRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

func (a anomalyResource) act(w http.ResponseWriter, r *http.Request) {
	id, ok := anomalyID(w, r)
	if !ok {
		return
	}
	if mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mediaType) != "application/json" {
		writeProblem(w, http.StatusUnsupportedMediaType, "send the action as application/json")
		return
	}
	var req actionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be JSON with action and an optional note")
		return
	}
	action, err := domain.ParseAction(req.Action)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "action must be CREATE_INSPECTION_ORDER, REQUEST_METER_VALIDATION, CONFIRM_OPERATION, DISMISS or RESOLVE")
		return
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > maxNoteLength {
		writeProblem(w, http.StatusBadRequest, fmt.Sprintf("note can have at most %d characters", maxNoteLength))
		return
	}

	updated, err := a.store.ApplyAction(r.Context(), id, currentUser(r).ID, action, note)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "no anomaly has that id")
		return
	case errors.Is(err, domain.ErrInvalidTransition):
		writeProblem(w, http.StatusConflict, "that action is not allowed in the current status of the anomaly")
		return
	case err != nil:
		serverError(w, a.log, "apply action", err)
		return
	}
	history, err := a.store.AnomalyActions(r.Context(), id)
	if err != nil {
		serverError(w, a.log, "read anomaly actions", err)
		return
	}
	writeJSON(w, http.StatusOK, toAnomalyDetail(updated, history))
}

// anomalyID reads the id from the path; one that is not a uuid cannot exist,
// so it is a 404 without going to the store.
func anomalyID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !uuidPattern.MatchString(id) {
		writeProblem(w, http.StatusNotFound, "no anomaly has that id")
		return "", false
	}
	return id, true
}
