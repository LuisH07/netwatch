package cmd

import (
	"errors"
	"fmt"
	"testing"
)

func TestResolveExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"exitError code 1", &exitError{code: 1}, 1},
		{"exitError code 2", &exitError{code: 2}, 2},
		{"exitError code 0", &exitError{code: 0}, 0},
		{"generic error falls back to 2", errors.New("boom"), 2},
		{"wrapped exitError", fmt.Errorf("wrap: %w", &exitError{code: 1}), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveExitCode(tt.err); got != tt.want {
				t.Errorf("resolveExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
