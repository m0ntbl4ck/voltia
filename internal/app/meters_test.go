package app

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// memoryMeters serves the shipped dataset as the source, the meter reader and
// the anomaly reader, with the anomalies the engine finds in it.
type memoryMeters struct {
	ds        seed.Dataset
	anomalies []domain.Anomaly
}

func newMemoryMeters(t *testing.T) *memoryMeters {
	t.Helper()
	m := newMemory(t)
	report, meters := datasetRun(t)
	out := &memoryMeters{ds: m.ds}
	for i, a := range report.Anomalies {
		rec, err := BuildAnomaly(a, meters[a.Episode.MeterID], domain.Explanation{Summary: "s", Reason: "r", RecommendedAction: "a"}, "run-1")
		if err != nil {
			t.Fatal(err)
		}
		rec.ID = "anomaly-" + string(rune('a'+i))
		out.anomalies = append(out.anomalies, rec)
	}
	return out
}

func (m *memoryMeters) service() *MeterService {
	return NewMeterService(m, m, m, analysis.DefaultConfig(), m.ds.Readings[0].Timestamp.Location())
}

func (m *memoryMeters) setStatus(meterID string, st domain.AnomalyStatus) {
	for i := range m.anomalies {
		if m.anomalies[i].MeterID == meterID {
			m.anomalies[i].Status = st
		}
	}
}

func (m *memoryMeters) Meters(context.Context) ([]domain.Meter, error)     { return m.ds.Meters, nil }
func (m *memoryMeters) Readings(context.Context) ([]domain.Reading, error) { return m.ds.Readings, nil }
func (m *memoryMeters) Events(context.Context) ([]domain.Event, error)     { return m.ds.Events, nil }

func (m *memoryMeters) Meter(_ context.Context, id string) (domain.Meter, error) {
	for _, x := range m.ds.Meters {
		if x.MeterID == id {
			return x, nil
		}
	}
	return domain.Meter{}, domain.ErrNotFound
}

func (m *memoryMeters) MeterReadings(_ context.Context, id string, from, to time.Time) ([]domain.Reading, error) {
	var out []domain.Reading
	for _, r := range m.ds.Readings {
		if r.MeterID == id && !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b domain.Reading) int { return a.Timestamp.Compare(b.Timestamp) })
	return out, nil
}

func (m *memoryMeters) MeterEvents(_ context.Context, id string) ([]domain.Event, error) {
	var out []domain.Event
	for _, e := range m.ds.Events {
		if e.MeterID == id {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memoryMeters) Anomalies(_ context.Context, f domain.AnomalyFilter) ([]domain.Anomaly, error) {
	var out []domain.Anomaly
	for _, a := range m.anomalies {
		if f.MeterID == "" || a.MeterID == f.MeterID {
			out = append(out, a)
		}
	}
	return out, nil
}

func codes(list []MeterSummary) []string {
	out := make([]string, len(list))
	for i, m := range list {
		out[i] = m.Meter.MeterID
	}
	return out
}

func find(t *testing.T, list []MeterSummary, id string) MeterSummary {
	t.Helper()
	for _, m := range list {
		if m.Meter.MeterID == id {
			return m
		}
	}
	t.Fatalf("meter %s is not in the list", id)
	return MeterSummary{}
}

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func TestMetersListsEveryMeterWithItsDerivedStatus(t *testing.T) {
	list, err := newMemoryMeters(t).service().Meters(context.Background(), MeterFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 12 || !slices.IsSorted(codes(list)) {
		t.Fatalf("%d meters, codes %v; want 12 ordered by code", len(list), codes(list))
	}
	wantStatus := map[string]domain.MeterStatus{"M-109": domain.MeterCritical, "M-112": domain.MeterAlert, "M-104": domain.MeterAlert, "M-106": domain.MeterOK}
	for _, m := range list {
		want, ok := wantStatus[m.Meter.MeterID]
		if !ok {
			want = domain.MeterOK
		}
		if m.Status != want {
			t.Errorf("%s is %s, want %s", m.Meter.MeterID, m.Status, want)
		}
	}
}

func TestSummaryOfTheSurgingMeter(t *testing.T) {
	list, _ := newMemoryMeters(t).service().Meters(context.Background(), MeterFilter{})
	m := find(t, list, "M-109")
	if !m.HasBaseline || !near(m.BaselineDailyKWh, 1048, 3) || !near(m.RecentDailyKWh, 2201, 5) || !near(m.VariationPct, 110, 1.5) {
		t.Errorf("baseline %.1f, recent %.1f, variation %.1f%%; the design says about 1.048, 2.201 and +110%%",
			m.BaselineDailyKWh, m.RecentDailyKWh, m.VariationPct)
	}
	if m.Unresolved != 1 || m.TopSeverity != domain.SeverityHigh {
		t.Errorf("unresolved %d, top severity %q", m.Unresolved, m.TopSeverity)
	}
	if m.Meter.Name != "Molino" {
		t.Errorf("name = %q", m.Meter.Name)
	}
	if m.LastReading.Hour() != 23 || m.LastReading.Day() != 14 || m.LastReadingState == "" {
		t.Errorf("last reading %v (%q)", m.LastReading, m.LastReadingState)
	}
}

func TestSparklineHasFourteenCompleteDays(t *testing.T) {
	list, _ := newMemoryMeters(t).service().Meters(context.Background(), MeterFilter{})
	m := find(t, list, "M-104")
	if len(m.Daily) != 14 || m.Daily[0].Date != "2026-09-01" || m.Daily[13].Date != "2026-09-14" {
		t.Fatalf("daily = %+v", m.Daily)
	}
	var total float64
	for _, d := range m.Daily {
		total += d.KWh
	}
	var want float64
	for _, r := range newMemoryMeters(t).ds.Readings {
		if r.MeterID == "M-104" {
			want += r.ConsumptionKWh
		}
	}
	if !near(total, want, 1e-6) {
		t.Errorf("the sparkline adds up to %.2f kWh, the readings to %.2f", total, want)
	}
	if !(m.Daily[13].KWh > 1.3*m.Daily[3].KWh) {
		t.Errorf("M-104 rose 47%% but its last day is %.0f against %.0f", m.Daily[13].KWh, m.Daily[3].KWh)
	}
}

func TestResolvedAnomaliesStopCounting(t *testing.T) {
	data := newMemoryMeters(t)
	data.setStatus("M-109", domain.StatusResolved)
	data.setStatus("M-104", domain.StatusDismissed)
	data.setStatus("M-112", domain.StatusAcknowledged)
	list, _ := data.service().Meters(context.Background(), MeterFilter{})

	if m := find(t, list, "M-109"); m.Status != domain.MeterOK || m.Unresolved != 0 || m.TopSeverity != "" {
		t.Errorf("M-109 = %s, %d unresolved, severity %q; want ok with none", m.Status, m.Unresolved, m.TopSeverity)
	}
	if m := find(t, list, "M-104"); m.Status != domain.MeterOK {
		t.Errorf("a dismissed anomaly left M-104 %s", m.Status)
	}
	if m := find(t, list, "M-112"); m.Status != domain.MeterAlert || m.Unresolved != 1 {
		t.Errorf("an acknowledged anomaly should still count: M-112 %s with %d", m.Status, m.Unresolved)
	}
}

func TestMeterFilters(t *testing.T) {
	svc := newMemoryMeters(t).service()
	ctx := context.Background()
	tests := []struct {
		name   string
		filter MeterFilter
		want   []string
	}{
		{"one status", MeterFilter{Statuses: []domain.MeterStatus{domain.MeterCritical}}, []string{"M-109"}},
		{"two statuses", MeterFilter{Statuses: []domain.MeterStatus{domain.MeterCritical, domain.MeterAlert}}, []string{"M-104", "M-109", "M-112"}},
		{"by name, any case", MeterFilter{Query: "MOLINO"}, []string{"M-109"}},
		{"by code", MeterFilter{Query: "m-11"}, []string{"M-110", "M-111", "M-112"}},
		{"by location", MeterFilter{Query: "nave 2"}, []string{"M-104", "M-106", "M-108"}},
		{"status and query", MeterFilter{Statuses: []domain.MeterStatus{domain.MeterAlert}, Query: "nave 2"}, []string{"M-104"}},
		{"no match", MeterFilter{Query: "zzz"}, nil},
		{"spaces around the query", MeterFilter{Query: "  molino "}, []string{"M-109"}},
	}
	for _, tt := range tests {
		got, err := svc.Meters(ctx, tt.filter)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(codes(got), append([]string(nil), tt.want...)) && !(len(got) == 0 && len(tt.want) == 0) {
			t.Errorf("%s: %v, want %v", tt.name, codes(got), tt.want)
		}
	}
}

func TestMeterSorting(t *testing.T) {
	svc := newMemoryMeters(t).service()
	ctx := context.Background()
	asc, desc := true, false

	byVariation, _ := svc.Meters(ctx, MeterFilter{Sort: SortVariation})
	if codes(byVariation)[0] != "M-109" || codes(byVariation)[1] != "M-104" {
		t.Errorf("by variation: %v, want M-109 then M-104 first", codes(byVariation)[:3])
	}
	byVariationAsc, _ := svc.Meters(ctx, MeterFilter{Sort: SortVariation, Ascending: &asc})
	if last := byVariationAsc[len(byVariationAsc)-1]; last.Meter.MeterID != "M-109" {
		t.Errorf("by variation ascending ends with %s, want M-109", last.Meter.MeterID)
	}
	for i := 1; i < len(byVariationAsc); i++ {
		if byVariationAsc[i-1].VariationPct > byVariationAsc[i].VariationPct {
			t.Errorf("by variation ascending is not ascending at %s", byVariationAsc[i].Meter.MeterID)
		}
	}

	byConsumption, _ := svc.Meters(ctx, MeterFilter{Sort: SortConsumption})
	for i := 1; i < len(byConsumption); i++ {
		if byConsumption[i-1].RecentDailyKWh < byConsumption[i].RecentDailyKWh {
			t.Errorf("by consumption is not descending at %s", byConsumption[i].Meter.MeterID)
		}
	}

	bySeverity, _ := svc.Meters(ctx, MeterFilter{Sort: SortSeverity})
	if got := codes(bySeverity)[:4]; !slices.Equal(got, []string{"M-109", "M-112", "M-104", "M-106"}) {
		t.Errorf("by severity: %v, want M-109, M-112, M-104, M-106 first", got)
	}

	byCodeDesc, _ := svc.Meters(ctx, MeterFilter{Ascending: &desc})
	if codes(byCodeDesc)[0] != "M-112" || codes(byCodeDesc)[11] != "M-101" {
		t.Errorf("by code descending: %v", codes(byCodeDesc))
	}
}

func TestMeterDetailShowsTheElectricalLevelsAndTheOpenAnomalies(t *testing.T) {
	data := newMemoryMeters(t)
	d, err := data.service().Meter(context.Background(), "M-109")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != domain.MeterCritical || !d.HasBaseline {
		t.Fatalf("detail = %s, baseline %v", d.Status, d.HasBaseline)
	}
	if !near(d.Current.Baseline, 202, 6) || !near(d.Current.Recent, 425, 8) {
		t.Errorf("current: recent %.1f baseline %.1f, want about 425 against 202", d.Current.Recent, d.Current.Baseline)
	}
	if !near(d.PowerFactor.Baseline, 0.94, 0.02) || !near(d.PowerFactor.Recent, 0.74, 0.02) {
		t.Errorf("power factor: recent %.3f baseline %.3f, want about 0.74 against 0.94", d.PowerFactor.Recent, d.PowerFactor.Baseline)
	}
	if !near(d.Voltage.Recent, d.Voltage.Baseline, 6) {
		t.Errorf("voltage moved: %.1f against %.1f", d.Voltage.Recent, d.Voltage.Baseline)
	}
	if len(d.Anomalies) != 1 || d.Anomalies[0].MeterID != "M-109" {
		t.Errorf("anomalies = %+v", d.Anomalies)
	}

	data.setStatus("M-109", domain.StatusResolved)
	d, _ = data.service().Meter(context.Background(), "M-109")
	if len(d.Anomalies) != 0 {
		t.Errorf("a resolved anomaly is still listed as open: %d", len(d.Anomalies))
	}
}

func TestUnknownMeterIsNotFound(t *testing.T) {
	svc := newMemoryMeters(t).service()
	ctx := context.Background()
	if _, err := svc.Meter(ctx, "M-999"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Meter: %v", err)
	}
	if _, err := svc.Events(ctx, "M-999"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Events: %v", err)
	}
	if _, err := svc.Readings(ctx, "M-999", ReadingsQuery{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Readings: %v", err)
	}
}

func TestAMeterWithTooLittleDataHasNoBaselineButStillListsIt(t *testing.T) {
	data := newMemoryMeters(t)
	loc := data.ds.Readings[0].Timestamp.Location()
	data.ds.Meters = append(data.ds.Meters, domain.Meter{MeterID: "M-200", Name: "Nuevo", Location: "Bodega"})
	for h := 0; h < 5; h++ {
		data.ds.Readings = append(data.ds.Readings, domain.Reading{
			MeterID: "M-200", Timestamp: time.Date(2026, 9, 14, h, 0, 0, 0, loc), ConsumptionKWh: 10, Status: "OK",
		})
	}
	svc := data.service()
	list, err := svc.Meters(context.Background(), MeterFilter{})
	if err != nil {
		t.Fatal(err)
	}
	m := find(t, list, "M-200")
	if m.HasBaseline || m.RecentDailyKWh != 0 || m.BaselineDailyKWh != 0 || m.VariationPct != 0 || len(m.Daily) != 0 || m.Status != domain.MeterOK {
		t.Errorf("M-200 = %+v", m)
	}
	if m.LastReading.Hour() != 4 {
		t.Errorf("last reading %v", m.LastReading)
	}
	d, err := svc.Meter(context.Background(), "M-200")
	if err != nil || d.HasBaseline || d.Current != (Level{}) {
		t.Errorf("detail = %+v, %v", d, err)
	}
	s, err := svc.Readings(context.Background(), "M-200", ReadingsQuery{IncludeBaseline: true})
	if err != nil || s.Baseline != nil || len(s.Points) != 5 {
		t.Errorf("readings = %d points, baseline %v, %v", len(s.Points), s.Baseline, err)
	}
}

func TestHourlyReadingsRespectTheRange(t *testing.T) {
	data := newMemoryMeters(t)
	svc := data.service()
	loc := svc.Location()
	ctx := context.Background()

	all, err := svc.Readings(ctx, "M-104", ReadingsQuery{})
	if err != nil || len(all.Points) != 336 || all.Resolution != ResolutionHour || all.Baseline != nil {
		t.Fatalf("all = %d points, %s, baseline %v, %v", len(all.Points), all.Resolution, all.Baseline, err)
	}
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, loc)
	day, err := svc.Readings(ctx, "M-104", ReadingsQuery{From: from, To: from.Add(24 * time.Hour)})
	if err != nil || len(day.Points) != 24 || !day.Points[0].Time.Equal(from) || day.Points[0].Hours != 1 {
		t.Fatalf("one day = %d points, %v", len(day.Points), err)
	}
	fromOnly, _ := svc.Readings(ctx, "M-104", ReadingsQuery{From: time.Date(2026, 9, 14, 0, 0, 0, 0, loc)})
	if len(fromOnly.Points) != 24 {
		t.Errorf("an open end returned %d points, want the last 24", len(fromOnly.Points))
	}
	toOnly, _ := svc.Readings(ctx, "M-104", ReadingsQuery{To: time.Date(2026, 9, 2, 0, 0, 0, 0, loc)})
	if len(toOnly.Points) != 24 {
		t.Errorf("an open start returned %d points, want the first 24", len(toOnly.Points))
	}
	empty, err := svc.Readings(ctx, "M-104", ReadingsQuery{From: time.Date(2027, 1, 1, 0, 0, 0, 0, loc)})
	if err != nil || empty.Points == nil || len(empty.Points) != 0 {
		t.Errorf("a range with no data = %v, %v; want an empty slice", empty.Points, err)
	}
}

func TestDailyReadingsAddUpTheDay(t *testing.T) {
	data := newMemoryMeters(t)
	svc := data.service()
	s, err := svc.Readings(context.Background(), "M-104", ReadingsQuery{Resolution: ResolutionDay})
	if err != nil || len(s.Points) != 14 || s.Resolution != ResolutionDay {
		t.Fatalf("%d points, %s, %v", len(s.Points), s.Resolution, err)
	}
	first := s.Points[0]
	if first.Hours != 24 || first.Time.Hour() != 0 || first.Time.Day() != 1 {
		t.Errorf("first day = %+v", first)
	}
	var kwh, volts float64
	for _, r := range data.ds.Readings {
		if r.MeterID == "M-104" && r.Timestamp.Day() == 1 {
			kwh += r.ConsumptionKWh
			volts += r.VoltageV
		}
	}
	if !near(first.ConsumptionKWh, kwh, 1e-6) || !near(first.VoltageV, volts/24, 1e-6) {
		t.Errorf("day 1 = %.3f kWh and %.3f V, want %.3f and %.3f", first.ConsumptionKWh, first.VoltageV, kwh, volts/24)
	}
}

func TestReadingsCanCarryTheBaselineBand(t *testing.T) {
	svc := newMemoryMeters(t).service()
	ctx := context.Background()
	s, err := svc.Readings(ctx, "M-109", ReadingsQuery{IncludeBaseline: true})
	if err != nil || s.Baseline == nil {
		t.Fatalf("baseline = %v, %v", s.Baseline, err)
	}
	b := s.Baseline
	if b.BandZ != 3 || !near(b.DailyKWh, 1048, 3) {
		t.Errorf("band %v, daily %.1f", b.BandZ, b.DailyKWh)
	}
	if b.ReferenceStart.Day() != 1 || b.ReferenceEnd.Sub(b.ReferenceStart) != 7*24*time.Hour {
		t.Errorf("reference window %v to %v", b.ReferenceStart, b.ReferenceEnd)
	}
	for _, v := range domain.Variables {
		bands, ok := b.Profile[v]
		if !ok {
			t.Errorf("no profile for %s", v)
			continue
		}
		for h, band := range bands {
			if band.Median <= 0 || band.Sigma <= 0 {
				t.Errorf("%s hour %d: median %v sigma %v", v, h, band.Median, band.Sigma)
			}
		}
	}
	list, _ := svc.Meters(ctx, MeterFilter{})
	if m := find(t, list, "M-109"); !near(b.DailyKWh, m.BaselineDailyKWh, 1e-9) {
		t.Errorf("the chart baseline %.3f differs from the list baseline %.3f", b.DailyKWh, m.BaselineDailyKWh)
	}
}

func TestReadingsRejectBadQueries(t *testing.T) {
	svc := newMemoryMeters(t).service()
	loc := svc.Location()
	ctx := context.Background()
	at := time.Date(2026, 9, 10, 0, 0, 0, 0, loc)
	if _, err := svc.Readings(ctx, "M-104", ReadingsQuery{Resolution: "week"}); !errors.Is(err, ErrBadResolution) {
		t.Errorf("resolution week: %v", err)
	}
	if _, err := svc.Readings(ctx, "M-104", ReadingsQuery{From: at, To: at.Add(-time.Hour)}); !errors.Is(err, ErrBadRange) {
		t.Errorf("inverted range: %v", err)
	}
	if _, err := svc.Readings(ctx, "M-104", ReadingsQuery{From: at, To: at}); !errors.Is(err, ErrBadRange) {
		t.Errorf("empty range: %v", err)
	}
}

func TestEventsOfAMeter(t *testing.T) {
	svc := newMemoryMeters(t).service()
	got, err := svc.Events(context.Background(), "M-106")
	if err != nil || len(got) != 1 || got[0].Type != domain.EventScheduledOutage || got[0].Duration != 12*time.Hour {
		t.Errorf("events = %+v, %v", got, err)
	}
	if none, err := svc.Events(context.Background(), "M-101"); err != nil || len(none) != 0 {
		t.Errorf("M-101 events = %+v, %v", none, err)
	}
}

func TestSparklineKeepsOnlyTheLastFourteenDays(t *testing.T) {
	data := newMemoryMeters(t)
	loc := data.ds.Readings[0].Timestamp.Location()
	data.ds.Meters = append(data.ds.Meters, domain.Meter{MeterID: "M-201", Name: "Largo", Location: "Bodega"})
	for d := 0; d < 20; d++ {
		for h := 0; h < 24; h++ {
			data.ds.Readings = append(data.ds.Readings, domain.Reading{
				MeterID: "M-201", Timestamp: time.Date(2026, 8, 25+d, h, 0, 0, 0, loc), ConsumptionKWh: float64(d + 1), Status: "OK",
			})
		}
	}
	list, err := data.service().Meters(context.Background(), MeterFilter{})
	if err != nil {
		t.Fatal(err)
	}
	m := find(t, list, "M-201")
	if len(m.Daily) != 14 || m.Daily[0].Date != "2026-08-31" || m.Daily[13].Date != "2026-09-13" {
		t.Errorf("daily = %d days from %v to %v, want the last 14 (Aug 31 to Sep 13)", len(m.Daily), m.Daily[0].Date, m.Daily[len(m.Daily)-1].Date)
	}
}

func TestTheTopSeverityIsTheWorstOfSeveralOpenAnomalies(t *testing.T) {
	data := newMemoryMeters(t)
	low := data.anomalies[0]
	low.ID, low.Fingerprint, low.Severity, low.Type = "extra-low", "extra-low", domain.SeverityLow, domain.DataQuality
	medium := low
	medium.ID, medium.Fingerprint, medium.Severity = "extra-medium", "extra-medium", domain.SeverityMedium
	// M-109 already has a high one; the others come after it and must not win.
	data.anomalies = append(data.anomalies, low, medium)
	list, _ := data.service().Meters(context.Background(), MeterFilter{})
	m := find(t, list, "M-109")
	if m.Unresolved != 3 || m.TopSeverity != domain.SeverityHigh {
		t.Errorf("M-109: %d unresolved, top severity %q; want 3 and HIGH", m.Unresolved, m.TopSeverity)
	}
}

func TestTiesInASortAreBrokenByMeterCode(t *testing.T) {
	data := newMemoryMeters(t)
	slices.Reverse(data.ds.Meters)
	list, _ := data.service().Meters(context.Background(), MeterFilter{Sort: SortSeverity})
	got := codes(list)
	// Eight meters have no anomaly and tie at the bottom; they must come out by code.
	rest := got[4:]
	if !slices.IsSorted(rest) {
		t.Errorf("the tied meters are not ordered by code: %v", rest)
	}
}
