package relocation

import (
	"io"
	"os"
	"time"

	"manga-manager/internal/config"
)

type RelocationPolicy struct {
	StorageProfile string                 `json:"storage_profile"`
	IOPolicy       config.StorageIOPolicy `json:"io_policy"`
}

type RelocationDetail struct {
	Ordinal            int64  `json:"ordinal"`
	Kind               string `json:"kind"`
	RecordType         string `json:"record_type"`
	RecordID           int64  `json:"record_id"`
	SourcePath         string `json:"source_path"`
	TargetPath         string `json:"target_path"`
	Reason             string `json:"reason"`
	ExpectedSize       int64  `json:"expected_size"`
	ActualSize         int64  `json:"actual_size"`
	ModifiedNS         int64  `json:"modified_ns"`
	RunID              int64  `json:"run_id,omitempty"`
	RunStatus          string `json:"run_status,omitempty"`
	CanCancel          bool   `json:"can_cancel"`
	ConflictRecordType string `json:"conflict_record_type,omitempty"`
	ConflictRecordID   int64  `json:"conflict_record_id,omitempty"`
	PolicyBefore       *RelocationPolicy `json:"policy_before,omitempty"`
	PolicyAfter        *RelocationPolicy `json:"policy_after,omitempty"`
}

type RelocationPlan struct {
	ID            int64              `json:"id"`
	LibraryID     int64              `json:"library_id"`
	LibraryName   string             `json:"library_name"`
	LibraryRoot   string             `json:"library_root"`
	SourceRoot    string             `json:"source_root"`
	TargetRoot    string             `json:"target_root"`
	RunID         int64              `json:"run_id"`
	Status        string             `json:"status"`
	SeriesCount   int                `json:"series_count"`
	BookCount     int                `json:"book_count"`
	BlockerCount  int                `json:"blocker_count"`
	AnomalyCount  int                `json:"anomaly_count"`
	PolicyCount   int                `json:"policy_count"`
	AnomalyCounts map[string]int     `json:"anomaly_counts"`
	Samples       []RelocationDetail `json:"samples"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	FactsDigest   string             `json:"-"`
	PoliciesJSON  string             `json:"-"`
}

type RelocationDetails struct {
	Items  []RelocationDetail `json:"items"`
	Total  int                `json:"total"`
	Offset int                `json:"offset"`
	Limit  int                `json:"limit"`
}

// FileSystem is the host filesystem boundary; callers configure it before
// starting checks. Open only tests accessibility and never reads archive bytes.
type FileSystem interface {
	Stat(string) (os.FileInfo, error)
	Open(string) (io.Closer, error)
}

type HostFiles struct{}

func (HostFiles) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }
func (HostFiles) Open(path string) (io.Closer, error)   { return os.Open(path) }
