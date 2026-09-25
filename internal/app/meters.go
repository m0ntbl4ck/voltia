package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// sparklineDays is how many complete days the small chart of a meter shows.
const sparklineDays = 14

// bandZ is how many robust deviations wide the baseline band is drawn, the
// threshold at which the shift detector starts to care.
const bandZ = 3.0

// Sort keys of the meter list.
const (
	SortConsumption = "consumption"
	SortVariation   = "variation"
	SortSeverity    = "severity"
)

// Resolutions of a readings series.
const (
	ResolutionHour = "hour"
	ResolutionDay  = "day"
)

// MeterService answers what is going on with each meter. The baseline is not
// stored, so it is rebuilt on each request from the readings, which is cheap
// at this size and always agrees with the latest data.
type MeterService struct {
	source    ports.Source
	meters    ports.MeterReader
	anomalies ports.AnomalyReader
	cfg       analysis.Config
	loc       *time.Location
}

// NewMeterService builds the service. loc is the plant time zone, which
// decides where a day starts.
func NewMeterService(source ports.Source, meters ports.MeterReader, anomalies ports.AnomalyReader, cfg analysis.Config, loc *time.Location) *MeterService {
	return &MeterService{source: source, meters: meters, anomalies: anomalies, cfg: cfg, loc: loc}
}

// Location is the plant time zone.
func (s *MeterService) Location() *time.Location { return s.loc }

// DailyKWh is the consumption of one complete day.
type DailyKWh struct {
	Date string
	KWh  float64
}

// MeterSummary is one row of the meter list.
type MeterSummary struct {
	Meter  domain.Meter
	Status domain.MeterStatus
	// HasBaseline is false when the meter has too few readings to learn one, and
	// then the consumption figures below are zero.
	HasBaseline      bool
	RecentDailyKWh   float64
	BaselineDailyKWh float64
	VariationPct     float64
	LastReading      time.Time
	LastReadingState string
	Unresolved       int
	TopSeverity      domain.Severity
	Daily            []DailyKWh
}

// Level is a measured value now and what it normally is.
type Level struct {
	Recent   float64
	Baseline float64
}

// MeterDetail is the meter screen: the summary plus the electrical variables
// and the anomalies still open.
type MeterDetail struct {
	MeterSummary
	Voltage, Current, PowerFactor Level
	Anomalies                     []domain.Anomaly
}

// MeterFilter narrows and orders the meter list. A zero value lists every
// meter by code.
type MeterFilter struct {
	Statuses []domain.MeterStatus
	// Query matches the code, the name or the location, ignoring case.
	Query string
	// Sort is one of the Sort constants, or empty for the meter code.
	Sort string
	// Ascending overrides the default direction: descending for a sort key, ascending by code.
	Ascending *bool
}

// Meters lists every meter with its derived status.
func (s *MeterService) Meters(ctx context.Context, f MeterFilter) ([]MeterSummary, error) {
	meters, err := s.source.Meters(ctx)
	if err != nil {
		return nil, err
	}
	readings, err := s.source.Readings(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.source.Events(ctx)
	if err != nil {
		return nil, err
	}
	anomalies, err := s.anomalies.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil {
		return nil, err
	}
	byMeter := map[string][]domain.Reading{}
	for _, r := range readings {
		byMeter[r.MeterID] = append(byMeter[r.MeterID], r)
	}
	anomaliesOf := map[string][]domain.Anomaly{}
	for _, a := range anomalies {
		anomaliesOf[a.MeterID] = append(anomaliesOf[a.MeterID], a)
	}

	var out []MeterSummary
	for _, m := range meters {
		sum, _ := s.summarize(m, byMeter[m.MeterID], events, anomaliesOf[m.MeterID])
		if matches(sum, f) {
			out = append(out, sum)
		}
	}
	sortMeters(out, f)
	return out, nil
}

// Meter returns the detail of one meter, or domain.ErrNotFound.
func (s *MeterService) Meter(ctx context.Context, meterID string) (MeterDetail, error) {
	m, err := s.meters.Meter(ctx, meterID)
	if err != nil {
		return MeterDetail{}, err
	}
	readings, err := s.allReadings(ctx, meterID)
	if err != nil {
		return MeterDetail{}, err
	}
	events, err := s.meters.MeterEvents(ctx, meterID)
	if err != nil {
		return MeterDetail{}, err
	}
	anomalies, err := s.anomalies.Anomalies(ctx, domain.AnomalyFilter{MeterID: meterID})
	if err != nil {
		return MeterDetail{}, err
	}
	sum, base := s.summarize(m, readings, events, anomalies)
	detail := MeterDetail{MeterSummary: sum}
	for _, a := range anomalies {
		if a.Unresolved() {
			detail.Anomalies = append(detail.Anomalies, a)
		}
	}
	if sum.HasBaseline {
		days := s.cfg.Baseline.RecentDays
		detail.Voltage = s.level(readings, base, domain.Voltage, days)
		detail.Current = s.level(readings, base, domain.Current, days)
		detail.PowerFactor = s.level(readings, base, domain.PowerFactor, days)
	}
	return detail, nil
}

// summarize builds the row of one meter. The baseline is returned too, or the
// zero value when the meter has none.
func (s *MeterService) summarize(m domain.Meter, readings []domain.Reading, events []domain.Event, anomalies []domain.Anomaly) (MeterSummary, baseline.Meter) {
	sum := MeterSummary{Meter: m, Status: domain.StatusOf(anomalies), Daily: []DailyKWh{}}
	for _, a := range anomalies {
		if !a.Unresolved() {
			continue
		}
		sum.Unresolved++
		if domain.SeverityRank(a.Severity) > domain.SeverityRank(sum.TopSeverity) {
			sum.TopSeverity = a.Severity
		}
	}
	if len(readings) == 0 {
		return sum, baseline.Meter{}
	}
	last := readings[0]
	for _, r := range readings {
		if r.Timestamp.After(last.Timestamp) {
			last = r
		}
	}
	sum.LastReading, sum.LastReadingState = last.Timestamp, last.Status

	days := completeDays(readings)
	if len(days) > sparklineDays {
		days = days[len(days)-sparklineDays:]
	}
	for _, d := range days {
		sum.Daily = append(sum.Daily, DailyKWh{Date: d.date, KWh: d.kwh})
	}

	base, err := analysis.MeterBaseline(m.MeterID, readings, events, s.cfg)
	if err != nil {
		return sum, baseline.Meter{}
	}
	recent, err := baseline.RecentDailyKWh(readings, s.cfg.Baseline.RecentDays)
	if err != nil {
		return sum, baseline.Meter{}
	}
	sum.HasBaseline = true
	sum.BaselineDailyKWh = base.DailyKWh()
	sum.RecentDailyKWh = recent
	sum.VariationPct = baseline.VariationPct(recent, sum.BaselineDailyKWh)
	return sum, base
}

// level compares the recent mean of a variable with the mean of its hourly medians.
func (s *MeterService) level(readings []domain.Reading, base baseline.Meter, v domain.Variable, days int) Level {
	var sum float64
	for _, h := range base.Profiles[v] {
		sum += h.Median
	}
	return Level{Recent: recentMean(readings, v, days), Baseline: sum / 24}
}

func matches(sum MeterSummary, f MeterFilter) bool {
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, sum.Status) {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		hay := strings.ToLower(sum.Meter.MeterID + " " + sum.Meter.Name + " " + sum.Meter.Location)
		return strings.Contains(hay, q)
	}
	return true
}

func sortMeters(list []MeterSummary, f MeterFilter) {
	desc := f.Sort != ""
	if f.Ascending != nil {
		desc = !*f.Ascending
	}
	key := func(m MeterSummary) float64 {
		switch f.Sort {
		case SortConsumption:
			return m.RecentDailyKWh
		case SortVariation:
			return m.VariationPct
		case SortSeverity:
			return float64(domain.SeverityRank(m.TopSeverity))
		}
		return 0
	}
	sort.SliceStable(list, func(i, j int) bool {
		if f.Sort == "" {
			if desc {
				return list[i].Meter.MeterID > list[j].Meter.MeterID
			}
			return list[i].Meter.MeterID < list[j].Meter.MeterID
		}
		ki, kj := key(list[i]), key(list[j])
		if ki == kj {
			return list[i].Meter.MeterID < list[j].Meter.MeterID
		}
		if desc {
			return ki > kj
		}
		return ki < kj
	})
}

// day is the readings of one calendar day of the plant.
type day struct {
	date     string
	count    int
	kwh      float64
	readings []domain.Reading
}

// groupDays splits readings by calendar day, oldest first.
func groupDays(readings []domain.Reading) []day {
	byDate := map[string]*day{}
	var order []string
	for _, r := range readings {
		key := r.Timestamp.Format("2006-01-02")
		d := byDate[key]
		if d == nil {
			d = &day{date: key}
			byDate[key] = d
			order = append(order, key)
		}
		d.count++
		d.kwh += r.ConsumptionKWh
		d.readings = append(d.readings, r)
	}
	sort.Strings(order)
	out := make([]day, len(order))
	for i, k := range order {
		out[i] = *byDate[k]
	}
	return out
}

// completeDays keeps the days with 24 readings: a partial day would look like a drop.
func completeDays(readings []domain.Reading) []day {
	return slices.DeleteFunc(groupDays(readings), func(d day) bool { return d.count != 24 })
}

// recentMean averages a variable over the last `days` complete days.
func recentMean(readings []domain.Reading, v domain.Variable, days int) float64 {
	complete := completeDays(readings)
	if len(complete) > days {
		complete = complete[len(complete)-days:]
	}
	var sum float64
	var n int
	for _, d := range complete {
		for _, r := range d.readings {
			sum += r.Value(v)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// allReadings loads every reading of a meter.
func (s *MeterService) allReadings(ctx context.Context, meterID string) ([]domain.Reading, error) {
	return s.meters.MeterReadings(ctx, meterID, time.Time{}, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
}

// Events lists the events of a meter, or domain.ErrNotFound for an unknown one.
func (s *MeterService) Events(ctx context.Context, meterID string) ([]domain.Event, error) {
	if _, err := s.meters.Meter(ctx, meterID); err != nil {
		return nil, err
	}
	return s.meters.MeterEvents(ctx, meterID)
}

// ErrBadRange is returned for a readings query with its end before its start.
var ErrBadRange = errors.New("the end of the range is before its start")

// ErrBadResolution is returned for a resolution other than hour or day.
var ErrBadResolution = fmt.Errorf("resolution must be %s or %s", ResolutionHour, ResolutionDay)

// ReadingsQuery selects a series of one meter. A zero From or To leaves that
// end open.
type ReadingsQuery struct {
	From, To        time.Time
	Resolution      string
	IncludeBaseline bool
}

// Point is one reading, or one day when the resolution is a day.
type Point struct {
	Time           time.Time
	ConsumptionKWh float64
	VoltageV       float64
	CurrentA       float64
	PowerFactor    float64
	// Hours is how many hourly readings the point adds up: 1 for an hour, up to 24 for a day.
	Hours int
}

// HourBand is the normal range of one hour of the day.
type HourBand struct {
	Median float64
	Sigma  float64
}

// BaselineInfo is what the chart needs to draw the band of normal behaviour.
type BaselineInfo struct {
	ReferenceStart time.Time
	ReferenceEnd   time.Time
	// BandZ is the width of the band in robust deviations: median ± BandZ * sigma.
	BandZ    float64
	DailyKWh float64
	// Profile has 24 bands per variable, indexed by hour of the day.
	Profile map[domain.Variable][24]HourBand
}

// Series is the readings of a meter over a range.
type Series struct {
	Resolution string
	Points     []Point
	Baseline   *BaselineInfo
}

// Readings returns a series of a meter, or domain.ErrNotFound for an unknown meter.
func (s *MeterService) Readings(ctx context.Context, meterID string, q ReadingsQuery) (Series, error) {
	if q.Resolution == "" {
		q.Resolution = ResolutionHour
	}
	if q.Resolution != ResolutionHour && q.Resolution != ResolutionDay {
		return Series{}, ErrBadResolution
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.To.After(q.From) {
		return Series{}, ErrBadRange
	}
	if _, err := s.meters.Meter(ctx, meterID); err != nil {
		return Series{}, err
	}
	from, to := q.From, q.To
	if to.IsZero() {
		to = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	readings, err := s.meters.MeterReadings(ctx, meterID, from, to)
	if err != nil {
		return Series{}, err
	}

	series := Series{Resolution: q.Resolution, Points: []Point{}}
	if q.Resolution == ResolutionHour {
		for _, r := range readings {
			series.Points = append(series.Points, Point{
				Time: r.Timestamp, ConsumptionKWh: r.ConsumptionKWh, VoltageV: r.VoltageV,
				CurrentA: r.CurrentA, PowerFactor: r.PowerFactor, Hours: 1,
			})
		}
	} else {
		for _, d := range groupDays(readings) {
			series.Points = append(series.Points, dayPoint(d))
		}
	}
	if q.IncludeBaseline {
		info, err := s.baselineInfo(ctx, meterID)
		if err != nil {
			return Series{}, err
		}
		series.Baseline = info
	}
	return series, nil
}

// dayPoint adds up the consumption of a day and averages the electrical variables.
func dayPoint(d day) Point {
	p := Point{Time: d.readings[0].Timestamp, ConsumptionKWh: d.kwh, Hours: d.count}
	p.Time = time.Date(p.Time.Year(), p.Time.Month(), p.Time.Day(), 0, 0, 0, 0, p.Time.Location())
	for _, r := range d.readings {
		p.VoltageV += r.VoltageV
		p.CurrentA += r.CurrentA
		p.PowerFactor += r.PowerFactor
	}
	n := float64(d.count)
	p.VoltageV, p.CurrentA, p.PowerFactor = p.VoltageV/n, p.CurrentA/n, p.PowerFactor/n
	return p
}

// baselineInfo rebuilds the baseline of a meter for the chart. A meter with too
// little data has none, which is reported as a nil baseline and not as an error.
func (s *MeterService) baselineInfo(ctx context.Context, meterID string) (*BaselineInfo, error) {
	readings, err := s.allReadings(ctx, meterID)
	if err != nil {
		return nil, err
	}
	events, err := s.meters.MeterEvents(ctx, meterID)
	if err != nil {
		return nil, err
	}
	base, err := analysis.MeterBaseline(meterID, readings, events, s.cfg)
	if err != nil {
		return nil, nil
	}
	start, end := baseline.ReferenceWindow(readings, s.cfg.Baseline.ReferenceDays)
	info := &BaselineInfo{
		ReferenceStart: start, ReferenceEnd: end, BandZ: bandZ, DailyKWh: base.DailyKWh(),
		Profile: map[domain.Variable][24]HourBand{},
	}
	for _, v := range domain.Variables {
		var bands [24]HourBand
		for h, stat := range base.Profiles[v] {
			bands[h] = HourBand{Median: stat.Median, Sigma: stat.Sigma}
		}
		info.Profile[v] = bands
	}
	return info, nil
}
