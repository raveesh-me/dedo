package web

import _ "example.com/shop/gen/go/shop/orders/v1/ordersv1connect"

// A module nested under the backend's path may import its internal/, so
// the BFF reaches the backend's own code through it.
import _ "example.com/shop/lib/rates"
