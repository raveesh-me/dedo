package orders

import (
	"context"

	"example.com/svc/gen/svcv1"
	"example.com/svc/internal/apierr"
)

// fake is a test double that returns what a test needs: not checked.
type fake struct{}

func (fake) GetOrder(ctx context.Context, req *svcv1.GetOrderRequest) (*svcv1.GetOrderResponse, error) { // want GetOrder:"always reason ANYTHING"
	return nil, apierr.NotFound("ANYTHING", "")
}
