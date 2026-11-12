// Package billing may not import the platform SDK.
package billing

import (
	"fmt"

	"example.com/platform-sdk/auth" // want `HOUSE_GO_NO_PLATFORM_SDK: internal/billing/billing.go imports example.com/platform-sdk/auth: Call the platform services directly. \(https://example.com/rules#sdk\)`
)

func Check() bool { fmt.Println(); return auth.Check() }
