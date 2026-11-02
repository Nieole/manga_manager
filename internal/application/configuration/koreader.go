package configuration

import (
	"manga-manager/internal/config"
	"strings"
)

// KOReaderSettings is the editable device-sync section, not a full settings document.
type KOReaderSettings struct {
	Enabled             bool
	BasePath            string
	AllowRegistration   bool
	MatchMode           string
	PathIgnoreExtension bool
}

// SaveKOReaderSettings validates and publishes only the supplied section using
// Manager's optimistic save and compensation, never file-reload semantics.
func (s *Service) SaveKOReaderSettings(version string, in KOReaderSettings) (Result, error) {
	in.BasePath = strings.TrimSpace(in.BasePath)
	if in.BasePath == "" {
		in.BasePath = "/koreader"
	}
	in.MatchMode = strings.ToLower(strings.TrimSpace(in.MatchMode))
	if in.MatchMode == "" {
		in.MatchMode = config.KOReaderMatchModeBinaryHash
	}
	issues := []config.ValidationIssue{}
	if !strings.HasPrefix(in.BasePath, "/") {
		issues = append(issues, config.ValidationIssue{Field: "koreader.base_path", Message: "Base path must start with /", Severity: "error"})
	}
	switch in.MatchMode {
	case config.KOReaderMatchModeBinaryHash, config.KOReaderMatchModeFilePath:
	default:
		issues = append(issues, config.ValidationIssue{Field: "koreader.match_mode", Message: "Invalid match mode", Severity: "error"})
	}
	validation := config.ValidationResult{Valid: len(issues) == 0, Issues: issues}
	if !validation.Valid {
		return Result{Validation: validation}, &Invalid{validation}
	}
	expected := s.manager.Snapshot()
	if version == "" {
		return Result{}, ErrVersionRequired
	}
	if version != s.manager.Version(expected) {
		return Result{}, config.ErrConflict
	}
	next := config.CloneConfig(expected)
	next.KOReader.Enabled = in.Enabled
	next.KOReader.BasePath = in.BasePath
	next.KOReader.AllowRegistration = in.AllowRegistration
	next.KOReader.MatchMode = in.MatchMode
	next.KOReader.PathIgnoreExtension = in.PathIgnoreExtension
	return s.SaveExpected(&expected, next)
}
