// Package legacy may import the platform SDK: allow_in names it.
package legacy

import "example.com/platform-sdk/auth"

func Check() bool { return auth.Check() }
