package config

import "testing"

func TestLLMConnectionNormalizesLegacyEndpointWithoutApplicationConfig(t *testing.T) {
	c := NormalizeLLMConnection(LLMConnection{Provider: "openai-legacy", Endpoint: "https://example.test/v1/chat/completions"})
	if c.Provider != "openai" || c.APIMode != "chat_completions" || c.BaseURL != "https://example.test" || c.RequestPath != "/v1/chat/completions" || c.Timeout != 120 {
		t.Fatalf("connection=%+v", c)
	}
	if got := LLMEndpoint(c); got != "https://example.test/v1/chat/completions" {
		t.Fatalf("endpoint=%s", got)
	}
}
