// Package orders serves OrderService's errors.
package orders

import (
	"context"

	"connectrpc.com/connect/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"example.com/svc/gen/svcv1"
	"example.com/svc/internal/apierr"
	"github.com/raveesh-me/dedo/events"
)

const reasonTaken = "ORDER_TAKEN"

type Server struct{ mode int }

// notFound always returns ORDER_NOT_FOUND, through a helper of this package.
func notFound() error { // want notFound:"always reason ORDER_NOT_FOUND"
	return apierr.NotFound("ORDER_NOT_FOUND", "no such order")
}

// localError passes reason on, in this package.
func localError(reason string) error { // want localError:"reason params 0"
	return apierr.New(connect.CodeNotFound, reason, "")
}

func (s *Server) GetOrder(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) {
	dynamic := req2reason(req)
	switch s.mode {
	case 0:
		return nil, apierr.NotFound("ORDER_NOT_FOUND", "declared")
	case 1:
		return nil, apierr.NotFound("ORDER_MISSING", "undeclared") // want `DEDO_GO_ERROR_REASON_DECLARED: GetOrder returns the error reason ORDER_MISSING, which /example.svc.v1.OrderService/GetOrder or /example.svc.v1.ArchiveService/GetOrder does not declare`
	case 2:
		return nil, apierr.Gone() // want `the error reason ORDER_GONE`
	case 3:
		return nil, apierr.OrderNotFound()
	case 4:
		return nil, notFound()
	case 5:
		return nil, apierr.FromStore(nil)
	case 6:
		return nil, apierr.Prefixed("ANYTHING")
	case 7:
		info := &errdetails.ErrorInfo{Reason: reasonTaken} // want `the error reason ORDER_TAKEN`
		return nil, connect.NewError(connect.CodeNotFound, "").WithDetail(info)
	case 8:
		return nil, apierr.NotFound(dynamic, "not a constant")
	case 9:
		//dedo:ignore DEDO_GO_ERROR_REASON_DECLARED the gateway declares ORDER_LOST for us
		return nil, apierr.NotFound("ORDER_LOST", "suppressed")
	case 10:
		return nil, localError("ORDER_LOCAL") // want `the error reason ORDER_LOCAL`
	case 11:
		return nil, apierr.Unavailable(events.ReasonNotPublished) // want `the error reason EVENT_NOT_PUBLISHED`
	case 12:
		return nil, apierr.Variadic("ORDER_VARIADIC")
	case 13:
		return nil, apierr.NotFound("ORDER_ARCHIVED", "ArchiveService declares it")
	case 14:
		return nil, s.wrap("ORDER_WRAPPED") // want `the error reason ORDER_WRAPPED`
	case 15:
		return nil, (*Server).wrap(s, "ORDER_EXPR") // want `the error reason ORDER_EXPR`
	case 16:
		return nil, generic(0, "ORDER_GENERIC") // want `the error reason ORDER_GENERIC`
	}
	return &svcv1.GetOrderResponse{}, nil
}

// wrap is a method that passes reason on.
func (s *Server) wrap(reason string) error { // want wrap:"reason params 0"
	return apierr.NotFound(reason, "")
}

// generic passes reason on, whatever T is.
func generic[T any](_ T, reason string) error { // want generic:"reason params 1"
	return apierr.NotFound(reason, "")
}

func req2reason(*svcv1.GetOrderRequest) string { return "" }

// CreateOrder declares EVENT_NOT_PUBLISHED.
func (s *Server) CreateOrder(ctx context.Context, req *svcv1.CreateOrderRequest) (*svcv1.CreateOrderResponse, error) {
	if s.mode == 0 {
		return nil, apierr.Unavailable(events.ReasonNotPublished)
	}
	return nil, apierr.NotFound("ORDER_EXISTS", "declared")
}

// ShipOrder declares no reasons at all.
func (s *Server) ShipOrder(ctx context.Context, req *svcv1.ShipOrderRequest) (*svcv1.ShipOrderResponse, error) { // want ShipOrder:"always reason ORDER_NOT_FOUND"
	return nil, apierr.NotFound("ORDER_NOT_FOUND", "undeclared here") // want `/example.svc.v1.OrderService/ShipOrder does not declare`
}

// GetLegacy serves a service that is not a dedo service: not checked.
func (s *Server) GetLegacy(ctx context.Context, req *svcv1.GetLegacyRequest) (*svcv1.GetLegacyResponse, error) { // want GetLegacy:"always reason ANYTHING"
	return nil, apierr.NotFound("ANYTHING", "")
}

// Lookup is no RPC's handler: not checked.
func (s *Server) Lookup(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) { // want Lookup:"always reason ANYTHING"
	return nil, apierr.NotFound("ANYTHING", "")
}

// WatchOrders is a server stream in the contract: not checked.
func (s *Server) WatchOrders(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) { // want WatchOrders:"always reason ANYTHING"
	return nil, apierr.NotFound("ANYTHING", "")
}
