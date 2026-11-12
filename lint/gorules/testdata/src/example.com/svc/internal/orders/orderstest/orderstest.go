// Package orderstest holds a fake OrderService for tests: not checked.
package orderstest

import (
	"context"
	"testing"

	"example.com/svc/gen/svcv1"
	"example.com/svc/internal/apierr"
)

type Fake struct{ T testing.TB }

func (Fake) GetOrder(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) { // want GetOrder:"always reason ANYTHING"
	return nil, apierr.NotFound("ANYTHING", "")
}
