package driver_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/raveesh-me/dedo/events/driver"
)

func TestPermanent(t *testing.T) {
	cause := errors.New("no such topic")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", cause, false},
		{"permanent", driver.Permanent(cause), true},
		{"wrapped permanent", fmt.Errorf("kafka: %w", driver.Permanent(cause)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driver.IsPermanent(tt.err); got != tt.want {
				t.Fatalf("IsPermanent = %v, want %v", got, tt.want)
			}
		})
	}
	if !errors.Is(driver.Permanent(cause), cause) {
		t.Fatal("Permanent hides its cause from errors.Is")
	}
	if driver.Permanent(nil) != nil {
		t.Fatal("Permanent(nil) is not nil")
	}
}
