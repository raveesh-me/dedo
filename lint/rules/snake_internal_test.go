package rules

import "testing"

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"order":     "order",
		"orderLine": "order_line",
		"OrderLine": "order_line",
		"SKU":       "sku",
		"APIKey":    "api_key",
		"skuV2":     "sku_v2",
		"HTTPRoute": "http_route",
	} {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
