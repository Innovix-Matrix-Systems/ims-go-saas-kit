package dev

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
)

// Chart data for the panels: aggregates over what the dashboard already
// stores (captured requests and exceptions, the delivery log). Every series
// is complete (empty hours and days are zero rows), so charts never have to
// guess at gaps.

const (
	statsRouteLimit     = 5
	statsExceptionHours = 24
	statsClassLimit     = 6
)

// statsWindows are the windows the Requests charts offer, in hours.
var statsWindows = []int{1, 24, 168}

// deliveryWindows are the windows the Deliveries charts offer, in days.
var deliveryWindows = []int{7, 14, 30}

// RequestStats is the GET /dev/api/requests/stats payload.
type RequestStats struct {
	WindowHours int         `json:"window_hours"`
	Status      StatusMix   `json:"status"`
	Busiest     []RouteStat `json:"busiest"`
	Slowest     []RouteStat `json:"slowest"`
}

// StatusMix counts requests by status class.
type StatusMix struct {
	Success      int64 `json:"success"`
	Redirect     int64 `json:"redirect"`
	ClientErrors int64 `json:"client_errors"`
	ServerErrors int64 `json:"server_errors"`
}

// RouteStat is one route's traffic in the window.
type RouteStat struct {
	Method   string  `json:"method"`
	Route    string  `json:"route"`
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	P95Ms    float64 `json:"p95_ms"`
}

// ExceptionStats is the GET /dev/api/exceptions/stats payload.
type ExceptionStats struct {
	// Hours is the last 24 hours, oldest first, one row per hour.
	Hours   []HourCount      `json:"hours"`
	Classes []ExceptionGroup `json:"classes"`
}

// HourCount is the number of exceptions in one hour.
type HourCount struct {
	Hour        time.Time `json:"hour"`
	Occurrences int64     `json:"occurrences"`
}

// DeliveryStats is the GET /dev/api/deliveries/stats payload.
type DeliveryStats struct {
	WindowDays int `json:"window_days"`
	// Days is one row per calendar day (UTC), oldest first.
	Days     []DeliveryCounts `json:"days"`
	Channels []DeliveryCounts `json:"channels"`
}

// DeliveryCounts is deliveries by status for one day or one channel (the
// other key is empty).
type DeliveryCounts struct {
	Day     *time.Time `json:"day,omitempty"`
	Channel string     `json:"channel,omitempty"`
	Sent    int64      `json:"sent"`
	Failed  int64      `json:"failed"`
	Queued  int64      `json:"queued"`
	Skipped int64      `json:"skipped"`
}

func (c *DeliveryCounts) add(status string, n int64) {
	switch status {
	case delivery.StatusSent:
		c.Sent += n
	case delivery.StatusFailed:
		c.Failed += n
	case delivery.StatusSkipped:
		c.Skipped += n
	default:
		c.Queued += n
	}
}

// StatsService builds the chart aggregates from the telemetry repository
// and the delivery service, filling empty hours and days with zero rows.
type StatsService struct {
	telemetry  TelemetryRepository
	deliveries *delivery.Service
	now        func() time.Time
}

// Requests returns the status mix and the busiest and slowest routes over
// the last hours.
func (s *StatsService) Requests(ctx context.Context, hours int) (RequestStats, error) {
	stats, err := s.telemetry.RequestStats(ctx, s.now().Add(-time.Duration(hours)*time.Hour), statsRouteLimit)
	if err != nil {
		return RequestStats{}, err
	}
	stats.WindowHours = hours
	return stats, nil
}

// Exceptions returns exceptions per hour over the last day and the classes
// seen in it.
func (s *StatsService) Exceptions(ctx context.Context) (ExceptionStats, error) {
	end := s.now().Truncate(time.Hour)
	start := end.Add(-(statsExceptionHours - 1) * time.Hour)
	rows, err := s.telemetry.ExceptionsByHour(ctx, start)
	if err != nil {
		return ExceptionStats{}, err
	}
	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.Hour.Unix()] = row.Occurrences
	}
	out := ExceptionStats{Hours: make([]HourCount, 0, statsExceptionHours)}
	for hour := start; !hour.After(end); hour = hour.Add(time.Hour) {
		out.Hours = append(out.Hours, HourCount{Hour: hour, Occurrences: counts[hour.Unix()]})
	}
	if out.Classes, err = s.telemetry.ExceptionGroups(ctx, start, statsClassLimit); err != nil {
		return ExceptionStats{}, err
	}
	return out, nil
}

// Deliveries returns deliveries by status per UTC day and per channel over
// the last days.
func (s *StatsService) Deliveries(ctx context.Context, days int) (DeliveryStats, error) {
	out := DeliveryStats{WindowDays: days, Days: []DeliveryCounts{}, Channels: []DeliveryCounts{}}
	if s.deliveries == nil {
		return out, nil
	}
	end := s.now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -(days - 1))
	rows, err := s.deliveries.CountByDay(ctx, start)
	if err != nil {
		return DeliveryStats{}, err
	}
	index := make(map[int64]int, days)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		at := day
		index[day.Unix()] = len(out.Days)
		out.Days = append(out.Days, DeliveryCounts{Day: &at})
	}
	channels := map[string]*DeliveryCounts{}
	for _, row := range rows {
		if i, ok := index[row.Day.Unix()]; ok {
			out.Days[i].add(row.Status, row.Count)
		}
		channel := channels[row.Channel]
		if channel == nil {
			channel = &DeliveryCounts{Channel: row.Channel}
			channels[row.Channel] = channel
		}
		channel.add(row.Status, row.Count)
	}
	for _, channel := range channels {
		out.Channels = append(out.Channels, *channel)
	}
	slices.SortFunc(out.Channels, func(a, b DeliveryCounts) int { return strings.Compare(a.Channel, b.Channel) })
	return out, nil
}

// windowParam reads an integer query parameter that must be one of allowed;
// empty means the first.
func windowParam(r *http.Request, name string, allowed []int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return allowed[0], nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || !slices.Contains(allowed, value) {
		choices := make([]string, len(allowed))
		for i, v := range allowed {
			choices[i] = strconv.Itoa(v)
		}
		return 0, apperr.Validation(map[string][]string{
			name: {"The " + name + " must be one of " + strings.Join(choices, ", ") + "."},
		})
	}
	return value, nil
}

func (h *Handler) requestStats(w http.ResponseWriter, r *http.Request) {
	hours, err := windowParam(r, "hours", statsWindows)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	stats, err := h.stats.Requests(r.Context(), hours)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) exceptionStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.stats.Exceptions(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) deliveryStats(w http.ResponseWriter, r *http.Request) {
	days, err := windowParam(r, "days", deliveryWindows)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	stats, err := h.stats.Deliveries(r.Context(), days)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}
