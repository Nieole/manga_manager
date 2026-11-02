package api

import (
	"fmt"
	"manga-manager/internal/application/configuration"
	"manga-manager/internal/config"
)

func (c *administrationHTTP) systemCapabilities() SystemCapabilitiesResponse {
	return SystemCapabilitiesResponse{
		SupportedScanFormats:     append([]string{}, config.SupportedScanFormats...),
		SupportedScanProfiles:    append([]string{}, config.SupportedScanProfiles...),
		SupportedLogLevels:       append([]string{}, config.SupportedLogLevels...),
		SupportedStorageProfiles: append([]string{}, config.SupportedStorageProfiles...),
		DefaultScanFormats:       config.DefaultScanFormatsCSV,
		DefaultScanInterval:      config.DefaultScanInterval,
		SupportedLLMProviders:    []string{"ollama", "openai"},
		SupportedLLMAPIModes:     []string{"responses", "chat_completions"},
	}
}
func (c *administrationHTTP) buildSystemConfigResponse(cfg config.Config, publication configuration.Publication) SystemConfigResponse {
	return SystemConfigResponse{
		Config:       cfg,
		Validation:   config.ValidateConfig(&cfg),
		Capabilities: c.systemCapabilities(),
		FieldEffects: config.FieldEffects(),
		Publication:  publication,
	}
}
func (c *administrationHTTP) persistConfig(cfg *config.Config) error {
	expected := c.currentConfig()
	return c.persistConfigExpected(&expected, cfg)
}
func (c *administrationHTTP) persistConfigExpected(expected, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	_, err := c.configuration.SaveExpected(expected, *cfg)
	return err
}
