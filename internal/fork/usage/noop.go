package usage

import "errors"

var ErrDisabled = errors.New("usage statistics are disabled")

type NoopRecorder struct{}

func (NoopRecorder) QueryEvents(QueryFilter) (EventsPage, error)      { return EventsPage{}, ErrDisabled }
func (NoopRecorder) QuerySummary(SummaryFilter) ([]SummaryRow, error) { return nil, ErrDisabled }
func (NoopRecorder) QueryFailures(QueryFilter) ([]FailureRow, error)  { return nil, ErrDisabled }
func (NoopRecorder) QueryFilters(QueryFilter) (FilterOptions, error) {
	return FilterOptions{}, ErrDisabled
}
func (NoopRecorder) QueryMetrics(QueryFilter) (Metrics, error) { return Metrics{}, ErrDisabled }
