package billing

import (
	//dedo:ignore HOUSE_GO_NO_PLATFORM_SDK the migration off the SDK ends in Q3
	"example.com/platform-sdk/auth"
)

func check() bool { return auth.Check() }
