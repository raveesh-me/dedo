// Package objectives checks the service level objectives a contract
// declares (dedo.v1.service's service_level and dedo.v1.method's), for the
// lint plugin and for dedo slo, which writes them as OpenSLO documents.
//
// It depends on no OpenTelemetry package, so the lint plugin can use it.
package objectives

import (
	"fmt"
	"slices"
	"time"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// DurationBuckets are the bucket boundaries, in seconds, that OpenTelemetry's
// semantic conventions advise for rpc.server.call.duration, and that package
// telemetry records it with. A latency threshold is one of them, so the
// share of calls within it is counted exactly.
var DurationBuckets = []float64{
	0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
}

// The sane ranges of the declared values.
const (
	minAvailability = 0.9     // one nine
	maxAvailability = 0.99999 // five nines
	minPercentile   = 0.5
	maxPercentile   = 0.9999
	maxWindowDays   = 90
	day             = 24 * time.Hour
)

// ServiceProblems says what is wrong with a service's level objectives. A
// nil level has none.
func ServiceProblems(sl *dedov1.ServiceLevel) []string {
	if sl == nil {
		return nil
	}
	problems := ObjectivesProblems(sl.GetObjectives())
	return append(problems, windowProblems(sl)...)
}

func windowProblems(sl *dedov1.ServiceLevel) []string {
	if sl.GetWindow() == nil {
		return []string{"service_level declares no window: set one in whole days, such as 28 days"}
	}
	w := sl.GetWindow().AsDuration()
	switch {
	case w <= 0 || w%day != 0:
		return []string{fmt.Sprintf("the window %s is not a whole number of days", w)}
	case w > maxWindowDays*day:
		return []string{fmt.Sprintf("the window of %d days is longer than %d days: an error budget that long reacts to nothing", w/day, maxWindowDays)}
	}
	return nil
}

// ObjectivesProblems says what is wrong with a set of objectives.
func ObjectivesProblems(o *dedov1.Objectives) []string {
	if o.GetAvailability() == 0 && len(o.GetLatency()) == 0 {
		return []string{"no objective: declare an availability, a latency, or both"}
	}
	var problems []string
	if a := o.GetAvailability(); a != 0 && (a < minAvailability || a > maxAvailability) {
		problems = append(problems, fmt.Sprintf("availability %v is not a share between %v and %v, such as 0.999", a, minAvailability, maxAvailability))
	}
	var seen []float64
	for _, l := range o.GetLatency() {
		problems = append(problems, latencyProblems(l)...)
		if slices.Contains(seen, l.GetPercentile()) {
			problems = append(problems, fmt.Sprintf("percentile %v has two latency objectives", l.GetPercentile()))
		}
		seen = append(seen, l.GetPercentile())
	}
	return problems
}

func latencyProblems(l *dedov1.LatencyObjective) []string {
	var problems []string
	if p := l.GetPercentile(); p < minPercentile || p > maxPercentile {
		problems = append(problems, fmt.Sprintf("percentile %v is not a share between %v and %v, such as 0.99", p, minPercentile, maxPercentile))
	}
	if l.GetThreshold() == nil {
		return append(problems, "a latency objective declares no threshold")
	}
	seconds := l.GetThreshold().AsDuration().Seconds()
	if !slices.Contains(DurationBuckets, seconds) {
		problems = append(problems, fmt.Sprintf("threshold %s is not a bucket boundary of rpc.server.call.duration (%v seconds), so the share within it cannot be counted exactly", l.GetThreshold().AsDuration(), DurationBuckets))
	}
	return problems
}

// MethodProblems says what is wrong with how an RPC counts toward its
// service's objectives. service is the service's level, or nil; streaming
// is set for an RPC whose duration is a stream's.
func MethodProblems(m *dedov1.MethodServiceLevel, service *dedov1.ServiceLevel, streaming bool) []string {
	switch level := m.GetLevel().(type) {
	case *dedov1.MethodServiceLevel_Exempt:
		if !level.Exempt {
			return []string{"exempt: false says nothing; remove service_level from the RPC"}
		}
		if service == nil {
			return []string{"the RPC is exempt from objectives its service does not declare"}
		}
	case *dedov1.MethodServiceLevel_Objectives:
		problems := ObjectivesProblems(level.Objectives)
		if service.GetWindow() == nil {
			problems = append(problems, "the RPC's objectives are measured over its service's window, and the service declares no service_level window")
		}
		if streaming && len(level.Objectives.GetLatency()) > 0 {
			problems = append(problems, "a streaming RPC's duration is its stream's: it can have no latency objective")
		}
		return problems
	}
	return nil
}
