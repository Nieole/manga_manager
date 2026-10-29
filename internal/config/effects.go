package config

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// FieldEffect describes when consumers read a published configuration field.
// Managed and compatibility inputs do not independently change active behavior.
type FieldEffect struct {
	Field string `json:"field"`
	Scope string `json:"scope"`
}

var fieldScopes = map[string]string{
	"server.host": "restart", "server.port": "restart", "server.allowed_origins": "restart",
	"server.trusted_proxies": "immediate", "server.cookie_secure": "immediate",
	"database.path": "restart", "library.paths": "managed",
	"library.storage_profile": "next_operation", "library.storage_policies": "next_operation",
	"cache.dir": "next_operation", "cache.page_disk_cache_enabled": "immediate", "cache.page_disk_cache_max_bytes": "next_operation",
	"logging.level":   "immediate",
	"scanner.workers": "next_operation", "scanner.scan_profile": "next_operation", "scanner.thumbnail_format": "next_operation",
	"scanner.waifu2x_path": "next_operation", "scanner.realcugan_path": "next_operation",
	"scanner.archive_pool_size": "immediate", "scanner.max_ai_concurrency": "immediate",
	"tasks.run_slots": "immediate", "tasks.retain_runs_per_task": "next_operation", "tasks.retain_terminal_run_days": "next_operation",
	"tasks.retain_sample_days": "next_operation", "tasks.sample_interval_seconds": "next_operation",
	"tasks.backoff_factor": "next_operation", "tasks.backoff_max_hours": "next_operation", "tasks.backoff_stop_after": "next_operation",
	"tasks.resume_after_restart": "restart",
	"ollama.endpoint":            "compatibility", "ollama.model": "compatibility",
	"llm.provider": "next_operation", "llm.api_mode": "next_operation", "llm.base_url": "next_operation", "llm.request_path": "next_operation",
	"llm.endpoint": "compatibility", "llm.model": "next_operation", "llm.api_key": "next_operation", "llm.timeout": "next_operation",
	"scrapers.mal_client_id": "next_operation", "scrapers.comicvine_api_key": "next_operation",
	"protocols.opds.enabled": "immediate", "protocols.mihon.enabled": "immediate",
	"koreader.enabled": "immediate", "koreader.base_path": "immediate", "koreader.allow_registration": "immediate",
	"koreader.match_mode": "next_operation", "koreader.path_ignore_extension": "next_operation",
}

func fieldScope(path string) string {
	if strings.HasPrefix(path, "library.io_policy.") || strings.HasPrefix(path, "library.storage_policies[]") {
		return "next_operation"
	}
	return fieldScopes[path]
}

// FieldEffects is the field-level timing catalog consumed by configuration clients.
func FieldEffects() []FieldEffect {
	var result []FieldEffect
	var visit func(reflect.Type, string)
	visit = func(t reflect.Type, path string) {
		for i := range t.NumField() {
			field := t.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			key := name
			if path != "" {
				key = path + "." + name
			}
			if field.Type.Kind() == reflect.Struct {
				visit(field.Type, key)
				continue
			}
			if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct {
				visit(field.Type.Elem(), key+"[]")
				continue
			}
			result = append(result, FieldEffect{Field: key, Scope: fieldScope(key)})
		}
	}
	visit(reflect.TypeFor[Config](), "")
	sort.Slice(result, func(i, j int) bool { return result[i].Field < result[j].Field })
	return result
}

// ChangedFieldEffects compares normalized configuration values without returning their contents.
func ChangedFieldEffects(before, after Config) []FieldEffect {
	flatten := func(cfg Config) map[string]any {
		data, _ := json.Marshal(cfg)
		var root map[string]any
		_ = json.Unmarshal(data, &root)
		result := make(map[string]any)
		var visit func(map[string]any, string)
		visit = func(m map[string]any, prefix string) {
			for key, value := range m {
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				if object, ok := value.(map[string]any); ok {
					visit(object, path)
				} else {
					result[path] = value
				}
			}
		}
		visit(root, "")
		return result
	}
	left, right := flatten(before), flatten(after)
	var result []FieldEffect
	for key, value := range right {
		if !reflect.DeepEqual(left[key], value) {
			result = append(result, FieldEffect{Field: key, Scope: fieldScope(key)})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Field < result[j].Field })
	return result
}
