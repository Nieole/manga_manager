package scanner

import (
	"testing"

	"manga-manager/internal/config"
)

func TestScannerPreventsDuplicateLibraryScans(t *testing.T) {
	s := NewScanner(nil, nil, config.NewManager(&config.Config{}))

	if !s.beginLibraryScan(1) {
		t.Fatal("expected first library scan to start")
	}
	if s.beginLibraryScan(1) {
		t.Fatal("expected duplicate library scan to be rejected")
	}

	s.endLibraryScan(1)

	if !s.beginLibraryScan(1) {
		t.Fatal("expected library scan to be allowed after release")
	}
}

func TestScannerPreventsDuplicateSeriesScans(t *testing.T) {
	s := NewScanner(nil, nil, config.NewManager(&config.Config{}))

	if !s.beginSeriesScan(42) {
		t.Fatal("expected first series scan to start")
	}
	if s.beginSeriesScan(42) {
		t.Fatal("expected duplicate series scan to be rejected")
	}

	s.endSeriesScan(42)

	if !s.beginSeriesScan(42) {
		t.Fatal("expected series scan to be allowed after release")
	}
}
