package logger

import "testing"

func TestSetLevelAndCurrentLevel(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		if err := SetLevel(level); err != nil {
			t.Fatalf("SetLevel(%q) returned error: %v", level, err)
		}
		if got := CurrentLevel(); got != level {
			t.Fatalf("expected CurrentLevel %q, got %q", level, got)
		}
	}
}

func TestSetLevelRejectsInvalidValue(t *testing.T) {
	if err := SetLevel("verbose"); err == nil {
		t.Fatal("expected invalid log level to return an error")
	}
}
