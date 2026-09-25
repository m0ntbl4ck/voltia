package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// maxQueryLength bounds the free text search of the meter list.
const maxQueryLength = 100

// meterIDPattern accepts the codes a meter can have and nothing that could be
// mistaken for something else in a path.
var meterIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// MeterAPI is what the meter endpoints ask of the application.
type MeterAPI interface {
	Meters(ctx context.Context, f app.MeterFilter) ([]app.MeterSummary, error)
	Meter(ctx context.Context, meterID string) (app.MeterDetail, error)
	Readings(ctx context.Context, meterID string, q app.ReadingsQuery) (app.Series, error)
	Events(ctx context.Context, meterID string) ([]domain.Event, error)
	// Location is the plant time zone, which decides where a date starts.
	Location() *time.Location
}

type meterResource struct {
	api MeterAPI
	log *slog.Logger
}

func (m meterResource) routes(r chi.Router) {
	r.Get("/meters", m.list)
	r.Get("/meters/{meter_id}", m.get)
	r.Get("/meters/{meter_id}/readings", m.readings)
	r.Get("/meters/{meter_id}/events", m.events)
}

type dailyResponse struct {
	Date string  `json:"date"`
	KWh  float64 `json:"kwh"`
}

type meterResponse struct {
	MeterID          string          `json:"meter_id"`
	Name             string          `json:"name"`
	Location         string          `json:"location"`
	Status           string          `json:"status"`
	HasBaseline      bool            `json:"has_baseline"`
	RecentDailyKWh   float64         `json:"recent_daily_kwh"`
	BaselineDailyKWh float64         `json:"baseline_daily_kwh"`
	VariationPct     float64         `json:"variation_pct"`
	LastReadingAt    *time.Time      `json:"last_reading_at"`
	LastReadingState string          `json:"last_reading_status"`
	OpenAnomalies    int             `json:"open_anomalies"`
	TopSeverity      *string         `json:"top_severity"`
	Daily            []dailyResponse `json:"daily"`
}

type levelResponse struct {
	Recent   float64 `json:"recent"`
	Baseline float64 `json:"baseline"`
}

type electricalResponse struct {
	VoltageV    levelResponse `json:"voltage_v"`
	CurrentA    levelResponse `json:"current_a"`
	PowerFactor levelResponse `json:"power_factor"`
}

type meterDetailResponse struct {
	meterResponse
	// Electrical is null when the meter has no baseline to compare with.
	Electrical *electricalResponse `json:"electrical"`
	Anomalies  []anomalyResponse   `json:"anomalies"`
}

func toMeterResponse(s app.MeterSummary) meterResponse {
	resp := meterResponse{
		MeterID: s.Meter.MeterID, Name: s.Meter.Name, Location: s.Meter.Location, Status: string(s.Status),
		HasBaseline: s.HasBaseline, RecentDailyKWh: s.RecentDailyKWh, BaselineDailyKWh: s.BaselineDailyKWh,
		VariationPct: s.VariationPct, LastReadingState: s.LastReadingState, OpenAnomalies: s.Unresolved,
		Daily: make([]dailyResponse, len(s.Daily)),
	}
	if !s.LastReading.IsZero() {
		t := s.LastReading.UTC()
		resp.LastReadingAt = &t
	}
	if s.TopSeverity != "" {
		sev := string(s.TopSeverity)
		resp.TopSeverity = &sev
	}
	for i, d := range s.Daily {
		resp.Daily[i] = dailyResponse{Date: d.Date, KWh: d.KWh}
	}
	return resp
}

func (m meterResource) list(w http.ResponseWriter, r *http.Request) {
	filter, msg := parseMeterFilter(r)
	if msg != "" {
		writeProblem(w, http.StatusBadRequest, msg)
		return
	}
	list, err := m.api.Meters(r.Context(), filter)
	if err != nil {
		serverError(w, m.log, "list meters", err)
		return
	}
	out := make([]meterResponse, len(list))
	for i, s := range list {
		out[i] = toMeterResponse(s)
	}
	writeJSON(w, http.StatusOK, out)
}

// parseMeterFilter reads ?status=&q=&sort=&order= and describes the first
// problem it finds, or returns an empty message.
func parseMeterFilter(r *http.Request) (app.MeterFilter, string) {
	q := r.URL.Query()
	var f app.MeterFilter
	if raw := q.Get("status"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			st := domain.MeterStatus(strings.ToLower(strings.TrimSpace(part)))
			if !oneOf(st, domain.MeterOK, domain.MeterAlert, domain.MeterCritical) {
				return f, "status must be a list of ok, alert and critical"
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	f.Query = q.Get("q")
	if utf8.RuneCountInString(f.Query) > maxQueryLength {
		return f, fmt.Sprintf("q can have at most %d characters", maxQueryLength)
	}
	if sortKey := q.Get("sort"); sortKey != "" {
		if !oneOf(sortKey, app.SortConsumption, app.SortVariation, app.SortSeverity) {
			return f, "sort must be consumption, variation or severity"
		}
		f.Sort = sortKey
	}
	switch q.Get("order") {
	case "":
	case "asc":
		asc := true
		f.Ascending = &asc
	case "desc":
		asc := false
		f.Ascending = &asc
	default:
		return f, "order must be asc or desc"
	}
	return f, ""
}

func (m meterResource) get(w http.ResponseWriter, r *http.Request) {
	id, ok := meterID(w, r)
	if !ok {
		return
	}
	d, err := m.api.Meter(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "no meter has that code")
		return
	}
	if err != nil {
		serverError(w, m.log, "read meter", err)
		return
	}
	resp := meterDetailResponse{meterResponse: toMeterResponse(d.MeterSummary), Anomalies: make([]anomalyResponse, len(d.Anomalies))}
	if d.HasBaseline {
		resp.Electrical = &electricalResponse{
			VoltageV:    levelResponse(d.Voltage),
			CurrentA:    levelResponse(d.Current),
			PowerFactor: levelResponse(d.PowerFactor),
		}
	}
	for i, a := range d.Anomalies {
		resp.Anomalies[i] = toAnomalyResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

type pointResponse struct {
	Timestamp      time.Time `json:"timestamp"`
	ConsumptionKWh float64   `json:"consumption_kwh"`
	VoltageV       float64   `json:"voltage_v"`
	CurrentA       float64   `json:"current_a"`
	PowerFactor    float64   `json:"power_factor"`
	Hours          int       `json:"hours"`
}

type bandResponse struct {
	Hour   int     `json:"hour"`
	Median float64 `json:"median"`
	Sigma  float64 `json:"sigma"`
}

type baselineResponse struct {
	ReferenceStart time.Time                 `json:"reference_start"`
	ReferenceEnd   time.Time                 `json:"reference_end"`
	BandZ          float64                   `json:"band_z"`
	DailyKWh       float64                   `json:"daily_kwh"`
	Profile        map[string][]bandResponse `json:"profile"`
}

type seriesResponse struct {
	MeterID    string            `json:"meter_id"`
	Resolution string            `json:"resolution"`
	Points     []pointResponse   `json:"points"`
	Baseline   *baselineResponse `json:"baseline"`
}

func (m meterResource) readings(w http.ResponseWriter, r *http.Request) {
	id, ok := meterID(w, r)
	if !ok {
		return
	}
	q, msg := parseReadingsQuery(r, m.api.Location())
	if msg != "" {
		writeProblem(w, http.StatusBadRequest, msg)
		return
	}
	series, err := m.api.Readings(r.Context(), id, q)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "no meter has that code")
		return
	case errors.Is(err, app.ErrBadRange), errors.Is(err, app.ErrBadResolution):
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		serverError(w, m.log, "read readings", err)
		return
	}
	resp := seriesResponse{MeterID: id, Resolution: series.Resolution, Points: make([]pointResponse, len(series.Points))}
	for i, p := range series.Points {
		resp.Points[i] = pointResponse{
			Timestamp: p.Time.UTC(), ConsumptionKWh: p.ConsumptionKWh, VoltageV: p.VoltageV,
			CurrentA: p.CurrentA, PowerFactor: p.PowerFactor, Hours: p.Hours,
		}
	}
	if b := series.Baseline; b != nil {
		resp.Baseline = &baselineResponse{
			ReferenceStart: b.ReferenceStart.UTC(), ReferenceEnd: b.ReferenceEnd.UTC(), BandZ: b.BandZ,
			DailyKWh: b.DailyKWh, Profile: map[string][]bandResponse{},
		}
		for v, bands := range b.Profile {
			out := make([]bandResponse, len(bands))
			for h, band := range bands {
				out[h] = bandResponse{Hour: h, Median: band.Median, Sigma: band.Sigma}
			}
			resp.Baseline.Profile[string(v)] = out
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseReadingsQuery reads ?from=&to=&resolution=&include=. A bare date means
// midnight in the plant time zone, and to is exclusive.
func parseReadingsQuery(r *http.Request, loc *time.Location) (app.ReadingsQuery, string) {
	q := r.URL.Query()
	var out app.ReadingsQuery
	var err error
	if out.From, err = parseTime(q.Get("from"), loc); err != nil {
		return out, "from must be an RFC 3339 time or a date like 2026-09-10"
	}
	if out.To, err = parseTime(q.Get("to"), loc); err != nil {
		return out, "to must be an RFC 3339 time or a date like 2026-09-10"
	}
	switch res := q.Get("resolution"); res {
	case "", app.ResolutionHour, app.ResolutionDay:
		out.Resolution = res
	default:
		return out, "resolution must be hour or day"
	}
	switch inc := q.Get("include"); inc {
	case "":
	case "baseline":
		out.IncludeBaseline = true
	default:
		return out, "include can only be baseline"
	}
	return out, ""
}

// parseTime reads an RFC 3339 time or a date in loc. Empty means unset.
func parseTime(s string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", s, loc)
}

type eventResponse struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
	// DurationHours is null when the event states no duration.
	DurationHours *float64 `json:"duration_hours"`
}

func (m meterResource) events(w http.ResponseWriter, r *http.Request) {
	id, ok := meterID(w, r)
	if !ok {
		return
	}
	events, err := m.api.Events(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "no meter has that code")
		return
	}
	if err != nil {
		serverError(w, m.log, "read events", err)
		return
	}
	out := make([]eventResponse, len(events))
	for i, e := range events {
		out[i] = eventResponse{Type: string(e.Type), Timestamp: e.Timestamp.UTC(), Description: e.Description}
		if e.Duration > 0 {
			h := e.Duration.Hours()
			out[i].DurationHours = &h
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// meterID reads the meter code from the path. One that could not be a code is
// a 404 without asking the application.
func meterID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "meter_id")
	if !meterIDPattern.MatchString(id) {
		writeProblem(w, http.StatusNotFound, "no meter has that code")
		return "", false
	}
	return id, true
}
