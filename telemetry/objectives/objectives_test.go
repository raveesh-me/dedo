package objectives_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/telemetry/objectives"
)

func latency(p float64, d time.Duration) *dedov1.LatencyObjective {
	return &dedov1.LatencyObjective{Percentile: p, Threshold: durationpb.New(d)}
}

func level(a float64, window time.Duration, lat ...*dedov1.LatencyObjective) *dedov1.ServiceLevel {
	sl := &dedov1.ServiceLevel{Objectives: &dedov1.Objectives{Availability: a, Latency: lat}}
	if window != 0 {
		sl.Window = durationpb.New(window)
	}
	return sl
}

const day = 24 * time.Hour

func TestServiceProblems(t *testing.T) {
	for name, tt := range map[string]struct {
		sl     *dedov1.ServiceLevel
		wantOK bool
	}{
		"availability":        {level(0.999, 28*day), true},
		"latency only":        {level(0, 28*day, latency(0.99, 250*time.Millisecond)), true},
		"both":                {level(0.9995, 30*day, latency(0.5, 50*time.Millisecond), latency(0.99, time.Second)), true},
		"none":                {nil, true},
		"no objectives":       {level(0, 28*day), false},
		"no window":           {level(0.999, 0), false},
		"partial day":         {level(0.999, 36*time.Hour), false},
		"window too long":     {level(0.999, 100*day), false},
		"availability 1":      {level(1, 28*day), false},
		"availability > 1":    {level(99.9, 28*day), false},
		"availability low":    {level(0.5, 28*day), false},
		"six nines":           {level(0.999999, 28*day), false},
		"percentile 1":        {level(0, 28*day, latency(1, time.Second)), false},
		"percentile low":      {level(0, 28*day, latency(0.1, time.Second)), false},
		"not a bucket":        {level(0, 28*day, latency(0.99, 300*time.Millisecond)), false},
		"no threshold":        {level(0, 28*day, &dedov1.LatencyObjective{Percentile: 0.99}), false},
		"repeated percentile": {level(0, 28*day, latency(0.99, time.Second), latency(0.99, 250*time.Millisecond)), false},
	} {
		problems := objectives.ServiceProblems(tt.sl)
		if (len(problems) == 0) != tt.wantOK {
			t.Errorf("%s: problems %v, want ok %v", name, problems, tt.wantOK)
		}
	}
}

func TestMethodProblems(t *testing.T) {
	exempt := &dedov1.MethodServiceLevel{Level: &dedov1.MethodServiceLevel_Exempt{Exempt: true}}
	own := func(o *dedov1.Objectives) *dedov1.MethodServiceLevel {
		return &dedov1.MethodServiceLevel{Level: &dedov1.MethodServiceLevel_Objectives{Objectives: o}}
	}
	svc := level(0.999, 28*day)
	for name, tt := range map[string]struct {
		m         *dedov1.MethodServiceLevel
		service   *dedov1.ServiceLevel
		streaming bool
		wantOK    bool
	}{
		"none":                       {nil, svc, false, true},
		"exempt":                     {exempt, svc, true, true},
		"own":                        {own(&dedov1.Objectives{Availability: 0.99, Latency: []*dedov1.LatencyObjective{latency(0.95, 2500*time.Millisecond)}}), svc, false, true},
		"exempt without service":     {exempt, nil, false, false},
		"own without window":         {own(&dedov1.Objectives{Availability: 0.99}), nil, false, false},
		"own latency on a stream":    {own(&dedov1.Objectives{Latency: []*dedov1.LatencyObjective{latency(0.95, time.Second)}}), svc, true, false},
		"own availability on stream": {own(&dedov1.Objectives{Availability: 0.99}), svc, true, true},
		"own with bad values":        {own(&dedov1.Objectives{Availability: 2}), svc, false, false},
		"own empty":                  {own(&dedov1.Objectives{}), svc, false, false},
		"exempt false":               {&dedov1.MethodServiceLevel{Level: &dedov1.MethodServiceLevel_Exempt{Exempt: false}}, svc, false, false},
	} {
		problems := objectives.MethodProblems(tt.m, tt.service, tt.streaming)
		if (len(problems) == 0) != tt.wantOK {
			t.Errorf("%s: problems %v, want ok %v", name, problems, tt.wantOK)
		}
	}
}

func TestBucketsAreTheSemanticConventions(t *testing.T) {
	b := objectives.DurationBuckets
	if len(b) != 18 || b[0] != 0.0001 || b[17] != 60 {
		t.Errorf("buckets = %v", b)
	}
}
