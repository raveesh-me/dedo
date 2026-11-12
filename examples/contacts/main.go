// Command contacts serves the ContactService and LocationService over
// Connect, gRPC and REST, and as MCP tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/raveesh-me/dedo/examples/contacts/app"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
	"github.com/raveesh-me/dedo/telemetry"
)

// serviceName is service.name when OTEL_SERVICE_NAME does not set it.
const serviceName = "contacts"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	enforce := flag.Bool("enforce-scopes", true, "reject calls without a declared scope; false only logs them")
	access := flag.String("mcp-access", "public", "who may reach /mcp: public, or authenticated (callers with a known token)")
	flag.Parse()

	// Every record logged with a call's context carries its trace_id and
	// span_id.
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(os.Stderr, nil)))
	cfg := app.Config{Log: log, EnforceScopes: *enforce, MCPAccess: mcpserver.Access(*access)}
	if err := run(log, *addr, cfg); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// run serves until SIGINT or SIGTERM, then drains requests and flushes
// telemetry.
func run(log *slog.Logger, addr string, cfg app.Config) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := setupTelemetry(ctx, log)
	if err != nil {
		return fmt.Errorf("startup failed: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, shutdownTelemetry(flushCtx))
	}()
	handler, err := app.NewHandler(cfg)
	if err != nil {
		return fmt.Errorf("startup failed: %w", err)
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	return serve(ctx, log, server)
}

// serve runs server until ctx is done, then shuts it down gracefully.
func serve(ctx context.Context, log *slog.Logger, server *http.Server) error {
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	log.Info("listening", "addr", server.Addr)
	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(drainCtx)
}

// setupTelemetry installs the OpenTelemetry SDK, which exports traces and
// metrics over OTLP (HTTP/protobuf). Only the standard OTEL_* environment
// variables configure it:
//
//	OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES   who is reporting
//	OTEL_EXPORTER_OTLP_ENDPOINT (and _HEADERS,    where to send, default
//	  _TIMEOUT, _COMPRESSION, per signal too)     http://localhost:4318
//	OTEL_TRACES_SAMPLER, OTEL_TRACES_SAMPLER_ARG  which traces to keep
//	OTEL_TRACES_EXPORTER, OTEL_METRICS_EXPORTER   otlp (default) or none
//	OTEL_PROPAGATORS                              tracecontext,baggage (default)
//	OTEL_SDK_DISABLED=true                        no SDK: every span is a no-op
//
// It returns the function that flushes and stops the SDK; call it on exit.
// On an error the service is meant to exit: what was installed stays
// global. This is the service's own code, not a dependency: change it
// freely, such as to add an exporter.
func setupTelemetry(ctx context.Context, log *slog.Logger) (func(context.Context) error, error) {
	// The propagator is set even without the SDK, so the caller's trace
	// context still reaches handlers, logs and outbound calls.
	propagator, err := propagatorFromEnv(os.Getenv("OTEL_PROPAGATORS"))
	if err != nil {
		return nil, err
	}
	otel.SetTextMapPropagator(propagator)
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return func(context.Context) error { return nil }, nil
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Warn("telemetry export failed", "error", err)
	}))
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", serviceName)),
		resource.WithFromEnv(), // OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES win
		resource.WithTelemetrySDK())
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}
	var shutdowns []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		for _, stop := range shutdowns {
			errs = append(errs, stop(ctx))
		}
		return errors.Join(errs...)
	}
	if on, err := exporterOn("TRACES"); err != nil {
		return nil, err
	} else if on {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("trace exporter: %w", err)
		}
		traces := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
		otel.SetTracerProvider(traces)
		shutdowns = append(shutdowns, traces.Shutdown)
	}
	if on, err := exporterOn("METRICS"); err != nil {
		return nil, errors.Join(err, shutdown(ctx))
	} else if on {
		exporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("metric exporter: %w", err), shutdown(ctx))
		}
		metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)), sdkmetric.WithResource(res))
		otel.SetMeterProvider(metrics)
		shutdowns = append(shutdowns, metrics.Shutdown)
	}
	return shutdown, nil
}

// exporterOn reads OTEL_<SIGNAL>_EXPORTER, otlp or none, and checks that
// the signal's OTLP protocol is http/protobuf, the one exporter built in.
func exporterOn(signal string) (bool, error) {
	switch exporter := os.Getenv("OTEL_" + signal + "_EXPORTER"); exporter {
	case "none":
		return false, nil
	case "", "otlp":
	default:
		return false, fmt.Errorf("OTEL_%s_EXPORTER=%s: this service exports otlp or none", signal, exporter)
	}
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_" + signal + "_PROTOCOL")
	if protocol == "" {
		protocol = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if protocol != "" && protocol != "http/protobuf" {
		return false, fmt.Errorf("OTLP protocol %s: this service exports http/protobuf; add the exporter for %s to main.go", protocol, protocol)
	}
	return true, nil
}

// propagatorFromEnv reads OTEL_PROPAGATORS: W3C Trace Context and Baggage
// by default.
func propagatorFromEnv(value string) (propagation.TextMapPropagator, error) {
	if value == "" {
		value = "tracecontext,baggage"
	}
	var propagators []propagation.TextMapPropagator
	for name := range strings.SplitSeq(value, ",") {
		switch strings.TrimSpace(name) {
		case "tracecontext":
			propagators = append(propagators, propagation.TraceContext{})
		case "baggage":
			propagators = append(propagators, propagation.Baggage{})
		case "none":
		default:
			return nil, fmt.Errorf("OTEL_PROPAGATORS: %q is not tracecontext, baggage or none", name)
		}
	}
	return propagation.NewCompositeTextMapPropagator(propagators...), nil
}
