package telemetry

import (
	"context"

	"connectrpc.com/connect/v2"
)

// Transport is how an RPC reached the server, the value of AttrTransport.
type Transport string

// The transports a dedo server is reached on.
const (
	// TransportConnect is the Connect protocol over HTTP.
	TransportConnect Transport = "connect"
	// TransportGRPC is gRPC.
	TransportGRPC Transport = "grpc"
	// TransportGRPCWeb is gRPC-Web.
	TransportGRPCWeb Transport = "grpc_web"
	// TransportREST is REST, transcoded from the google.api.http bindings
	// by Vanguard.
	TransportREST Transport = "rest"
	// TransportMCP is an MCP tool call, which package mcpserver makes
	// in-process.
	TransportMCP Transport = "mcp"
	// TransportInProcess is any other in-process call.
	TransportInProcess Transport = "in_process"
)

// protocolTransports maps CallInfo.Protocol to a Transport.
var protocolTransports = map[string]Transport{
	connect.ProtocolNameConnect: TransportConnect,
	connect.ProtocolNameGRPC:    TransportGRPC,
	connect.ProtocolNameGRPCWeb: TransportGRPCWeb,
	"rest":                      TransportREST, // Vanguard's protocol name
}

type transportKey struct{}

// WithTransport marks the in-process calls made with ctx as reaching the
// server on transport, such as TransportMCP. Package mcpserver marks its
// tool calls. The server interceptor reads the mark only on an in-process
// call, and removes it from the handler's context, so the handler's own
// in-process calls are not marked.
func WithTransport(ctx context.Context, transport Transport) context.Context {
	return context.WithValue(ctx, transportKey{}, transport)
}

// transportOf returns how the call reached the server, and ctx without the
// in-process mark.
func transportOf(ctx context.Context, protocol string) (Transport, context.Context) {
	if protocol != "" {
		if t, ok := protocolTransports[protocol]; ok {
			return t, ctx
		}
		return Transport(protocol), ctx
	}
	marked, ok := ctx.Value(transportKey{}).(Transport)
	if !ok || marked == "" {
		return TransportInProcess, ctx
	}
	return marked, context.WithValue(ctx, transportKey{}, Transport(""))
}
