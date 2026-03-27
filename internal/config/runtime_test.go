package config

import "testing"

func TestManagerSnapshotAndReplace(t *testing.T) {
	initial := &Config{}
	initial.Server.Port = 8080
	initial.Cache.Dir = "./data/cache"

	manager := NewManager(initial)
	snapshot := manager.Snapshot()
	if snapshot.Server.Port != 8080 {
		t.Fatalf("expected initial port 8080, got %d", snapshot.Server.Port)
	}

	updated := &Config{}
	updated.Server.Port = 9090
	updated.Cache.Dir = "./tmp/cache"
	manager.Replace(updated)

	snapshot = manager.Snapshot()
	if snapshot.Server.Port != 9090 {
		t.Fatalf("expected updated port 9090, got %d", snapshot.Server.Port)
	}
	if snapshot.Cache.Dir != "./tmp/cache" {
		t.Fatalf("expected updated cache dir, got %q", snapshot.Cache.Dir)
	}
}
