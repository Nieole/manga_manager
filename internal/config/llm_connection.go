package config

import "strings"

// LLMConnection contains only endpoint routing and request timeout configuration.
type LLMConnection struct {
	Provider, APIMode, BaseURL, RequestPath, Endpoint string
	Timeout                                           int
}

// NormalizeLLMConnection resolves legacy endpoint input without application defaults.
func NormalizeLLMConnection(c LLMConnection) LLMConnection {

	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	if provider == "" {
		provider = "ollama"
		c.Provider = provider
	}

	if c.BaseURL == "" && c.Endpoint != "" {
		c.BaseURL, c.RequestPath = splitEndpoint(c.Endpoint)
	}

	switch provider {
	case "openai-legacy":
		c.Provider = "openai"
		if c.APIMode == "" {
			c.APIMode = "chat_completions"
		}
	case "openai":
		if c.APIMode == "" {
			c.APIMode = inferAPIModeFromRequestPath(c.RequestPath)
		}
	default:
		c.APIMode = ""
		c.RequestPath = ""
	}

	if c.BaseURL == "" {
		if c.Provider == "openai" {
			c.BaseURL = "https://api.openai.com"
		} else {
			c.BaseURL = "http://localhost:11434"
		}
	}

	if c.Provider == "openai" && c.RequestPath == "" {
		c.RequestPath = defaultRequestPath(c.APIMode)
	}

	c.Endpoint = LLMEndpoint(c)

	if c.Timeout <= 0 {
		c.Timeout = 120
	}
	return c
}

// LLMEndpoint builds the selected provider endpoint.
func LLMEndpoint(c LLMConnection) string {

	baseURL := strings.TrimSpace(c.BaseURL)
	if baseURL == "" {
		return ""
	}
	if c.Provider != "openai" {
		return strings.TrimRight(baseURL, "/")
	}

	requestPath := strings.TrimSpace(c.RequestPath)
	if requestPath == "" {
		requestPath = defaultRequestPath(c.APIMode)
	}

	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
}
