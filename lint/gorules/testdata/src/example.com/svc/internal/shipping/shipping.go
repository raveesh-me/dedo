// Package shipping publishes OrderService's events.
package shipping

import (
	"context"

	"example.com/svc/gen/svcv1"
	"example.com/svc/internal/apierr"
	"github.com/raveesh-me/dedo/events"
)

const created = "example.svc.order.created"

type Server struct{ events *events.Publisher }

// publishLocal passes eventType on, in this package.
func (s *Server) publishLocal(ctx context.Context, eventType string) { // want publishLocal:"event params 1"
	_, _ = s.events.Publish(ctx, eventType, "orders/1")
}

func (s *Server) CreateOrder(ctx context.Context, req *svcv1.CreateOrderRequest) (*svcv1.CreateOrderResponse, error) {
	if _, err := s.events.Publish(ctx, created, "orders/1"); err != nil {
		return nil, err
	}
	_, _ = s.events.Publish(ctx, "example.svc.order.deleted", "orders/1")      // want `DEDO_GO_EVENT_DECLARED: CreateOrder publishes the event type example.svc.order.deleted, which /example.svc.v1.OrderService/CreateOrder does not declare in emits`
	_ = apierr.Publish(ctx, s.events, "example.svc.order.shipped", "orders/1") // want `the event type example.svc.order.shipped`
	s.publishLocal(ctx, "example.svc.order.local")                             // want `the event type example.svc.order.local`
	s.publishLocal(ctx, created)
	kind := req2type(req)
	_, _ = s.events.Publish(ctx, kind, "orders/1")
	//dedo:ignore DEDO_GO_EVENT_DECLARED published for the migration only, declared in v2
	_, _ = s.events.Publish(ctx, "example.svc.order.migrated", "orders/1")
	return &svcv1.CreateOrderResponse{}, nil
}

// GetOrder declares no emits.
func (s *Server) GetOrder(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) {
	_, _ = s.events.Publish(ctx, "example.svc.order.restored", "orders/1") // ArchiveService/GetOrder declares it
	_, _ = s.events.Publish(ctx, created, "orders/1")                      // want `/example.svc.v1.OrderService/GetOrder or /example.svc.v1.ArchiveService/GetOrder does not declare in emits`
	return nil, nil
}

// Notify is no RPC's handler: not checked.
func (s *Server) Notify(ctx context.Context) {
	_, _ = s.events.Publish(ctx, "example.svc.order.anything", "orders/1")
}

func req2type(*svcv1.CreateOrderRequest) string { return "" }
