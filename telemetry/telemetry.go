// Package telemetry instruments a dedo service with OpenTelemetry: a span
// and a duration metric for every RPC on every transport, W3C Trace Context
// and Baggage propagation, and a log handler that puts the trace on every
// record.
//
// It follows OpenTelemetry's semantic conventions for Connect RPC
// (rpc.system.name "connectrpc", rpc.method, rpc.status_code, error.type,
// rpc.server.call.duration and rpc.client.call.duration) and adds dedo's
// own attributes in the dedo namespace: the transport the call came in on,
// the flow ID and the declared ErrorInfo reason.
//
// The package depends only on OpenTelemetry's API. Until the service
// installs an SDK, every span and metric is a no-op that costs next to
// nothing; the trace context of the caller still reaches handlers and logs.
// The service's own main.go installs the SDK and its exporters, configured
// by the standard OTEL_* environment variables, as dedo init writes it.
//
// Request payloads never reach spans, metrics or logs from this package.
package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ScopeName is the instrumentation scope of every span and metric this
// package records.
const ScopeName = "github.com/raveesh-me/dedo/telemetry"

// dedo's own attributes, in the dedo namespace.
const (
	// AttrTransport is the transport an RPC reached the server on: see
	// Transport.
	AttrTransport = attribute.Key("dedo.rpc.transport")
	// AttrFlowID is the call's flow ID (package flowid). Spans only: it is
	// unique per request, too many values for a metric.
	AttrFlowID = attribute.Key("dedo.flow_id")
	// AttrErrorReason is the reason of the google.rpc.ErrorInfo detail of
	// a failed RPC, which the RPC declares in its errors.
	AttrErrorReason = attribute.Key("dedo.error.reason")
)

// The semantic conventions' attributes this package records.
const (
	attrRPCSystem      = attribute.Key("rpc.system.name")
	attrRPCMethod      = attribute.Key("rpc.method")
	attrRPCStatusCode  = attribute.Key("rpc.status_code")
	attrErrorType      = attribute.Key("error.type")
	attrServerAddress  = attribute.Key("server.address")
	attrServerPort     = attribute.Key("server.port")
	attrPeerAddress    = attribute.Key("network.peer.address")
	attrPeerPort       = attribute.Key("network.peer.port")
	attrHTTPMethod     = attribute.Key("http.request.method")
	attrHTTPRoute      = attribute.Key("http.route")
	attrHTTPStatusCode = attribute.Key("http.response.status_code")
	attrURLPath        = attribute.Key("url.path")
	attrURLScheme      = attribute.Key("url.scheme")
)

// rpcSystem is rpc.system.name for every transport: they all run on
// connect-go, and AttrTransport tells them apart.
const rpcSystem = "connectrpc"

// Config wires the instrumentation. Every field is optional.
type Config struct {
	// TracerProvider defaults to otel.GetTracerProvider().
	TracerProvider trace.TracerProvider
	// MeterProvider defaults to otel.GetMeterProvider().
	MeterProvider metric.MeterProvider
	// Propagator defaults to otel.GetTextMapPropagator().
	Propagator propagation.TextMapPropagator
}

// withDefaults returns a copy of cfg with the global providers in place of
// the missing ones. The globals delegate to whatever main installs later,
// so an interceptor built before the SDK still reaches it.
func (cfg Config) withDefaults() Config {
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	if cfg.MeterProvider == nil {
		cfg.MeterProvider = otel.GetMeterProvider()
	}
	if cfg.Propagator == nil {
		cfg.Propagator = otel.GetTextMapPropagator()
	}
	return cfg
}

func (cfg Config) tracer() trace.Tracer {
	return cfg.TracerProvider.Tracer(ScopeName)
}

func (cfg Config) meter() metric.Meter {
	return cfg.MeterProvider.Meter(ScopeName)
}
