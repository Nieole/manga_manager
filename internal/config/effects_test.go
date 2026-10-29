package config_test

import (
	"manga-manager/internal/config"
	"testing"
)

func TestFieldTimingCatalogCoversConfigurationAndKeepsStartupFieldsExplicit(t *testing.T) {
	fields := make(map[string]string)
	for _, field := range config.FieldEffects() {
		if field.Scope == "" {
			t.Errorf("configuration field has no timing: %s", field.Field)
		}
		fields[field.Field] = field.Scope
	}
	for field, want := range map[string]string{"server.port": "restart", "database.path": "restart", "tasks.resume_after_restart": "restart", "logging.level": "immediate", "server.cookie_secure": "immediate", "scanner.workers": "next_operation", "llm.api_key": "next_operation", "library.storage_policies[].io_policy.hash_concurrency": "next_operation", "library.paths": "managed", "ollama.endpoint": "compatibility"} {
		if fields[field] != want {
			t.Errorf("%s = %q want %q", field, fields[field], want)
		}
	}
}
