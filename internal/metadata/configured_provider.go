package metadata

import (
	"manga-manager/internal/config"
	"strings"
)

// NewConfiguredProvider resolves the source key using the current runtime configuration.
func NewConfiguredProvider(cfg config.Config, name string) Provider {
	switch strings.ToLower(name) {
	case "ollama", "llm", "openai", "openai-legacy":
		provider := cfg.LLM.Provider
		model := cfg.LLM.Model
		apiKey := cfg.LLM.APIKey
		return NewAIProvider(provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, model, apiKey, cfg.LLM.Timeout)
	case "anilist":
		return NewAniListProvider()
	case "mangadex":
		return NewMangaDexProvider()
	case "myanimelist", "mal":
		return NewMyAnimeListProvider(cfg.Scrapers.MALClientID)
	case "comicvine", "comic-vine", "comic_vine":
		return NewComicVineProvider(cfg.Scrapers.ComicVineAPIKey)
	default:
		return NewBangumiProvider()
	}
}
