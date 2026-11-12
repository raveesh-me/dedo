package billing

import (
	"testing"

	sdk "example.com/platform-sdk/auth" // want `HOUSE_GO_NO_PLATFORM_SDK`
)

func TestCheck(t *testing.T) { _ = sdk.Check() }
