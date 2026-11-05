package orders

import _ "example.com/shop/gen/go/shop/billing/v1/billingv1connect"
import _ "example.com/shop/gen/go/shop/orders/v1"
import _ "example.com/shop/internal/shared/money"

// count reads the module's own schema, and the shared one.
const count = "SELECT count(*) FROM orders.orders JOIN platform.audit ON true"
