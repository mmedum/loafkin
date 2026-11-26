package worker

import (
	"testing"
	"time"
)

func TestRetryDelayDoublesUpToTenMinutes(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{6, 320 * time.Second},
		{7, 10 * time.Minute},
		{40, 10 * time.Minute},
	}
	for _, tt := range tests {
		if got := retryDelay(tt.attempts); got != tt.want {
			t.Errorf("retryDelay(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}
