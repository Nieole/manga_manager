package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

type Service struct {
	Store  database.Store
	Config *config.Manager
	Files  FileSystem
	once   sync.Once
	err    error
}

func NewService(store database.Store, cfg *config.Manager) *Service {
	return &Service{Store: store, Config: cfg, Files: HostFiles{}}
}

// Initialize is idempotent for existing databases. Plans deliberately have no
// cascading dependency on run history: pruning runs must preserve their facts.
func (s *Service) Initialize(ctx context.Context) error {
	s.once.Do(func() {
		_, s.err = s.Store.DB().ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS relocation_plans (
 id INTEGER PRIMARY KEY AUTOINCREMENT, library_id INTEGER NOT NULL,
 run_id INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
 summary TEXT NOT NULL, facts_digest TEXT NOT NULL DEFAULT '', policies_json TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_relocation_library ON relocation_plans(library_id,id);
CREATE TABLE IF NOT EXISTS relocation_details (
 plan_id INTEGER NOT NULL, ordinal INTEGER NOT NULL, kind TEXT NOT NULL,
 detail TEXT NOT NULL, PRIMARY KEY(plan_id,ordinal),
 FOREIGN KEY(plan_id) REFERENCES relocation_plans(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_relocation_details_kind ON relocation_details(plan_id,kind,ordinal);`)
	})
	return s.err
}

func (s *Service) Create(ctx context.Context, libraryID int64, source, target string) (RelocationPlan, error) {
	if err := s.Initialize(ctx); err != nil {
		return RelocationPlan{}, err
	}
	lib, err := s.Store.GetLibrary(ctx, libraryID)
	if err != nil {
		return RelocationPlan{}, err
	}
	if source == "" {
		source = lib.Path
	}
	now := time.Now().UTC()
	plan := RelocationPlan{LibraryID: lib.ID, LibraryName: lib.Name, LibraryRoot: lib.Path,
		SourceRoot: source, TargetRoot: target, Status: "pending", AnomalyCounts: map[string]int{},
		Samples: []RelocationDetail{}, CreatedAt: now, UpdatedAt: now}
	body, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	result, err := s.Store.DB().ExecContext(ctx, `INSERT INTO relocation_plans(library_id,status,summary) VALUES(?,?,?)`, libraryID, plan.Status, body)
	if err != nil {
		return plan, err
	}
	plan.ID, err = result.LastInsertId()
	return plan, err
}

func (s *Service) SetRun(ctx context.Context, id, runID int64) error {
	_, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET run_id=? WHERE id=?`, runID, id)
	return err
}

func (s *Service) SetStatus(ctx context.Context, id int64, status string) error {
	_, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET status=? WHERE id=?`, status, id)
	return err
}

// InterruptChecks runs during controller assembly, before task recovery and
// background admission. A read-only check has no work to replay after a crash.
func (s *Service) InterruptChecks(ctx context.Context) error {
	if err := s.Initialize(ctx); err != nil {
		return err
	}
	_, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET status='interrupted' WHERE status IN ('pending','checking')`)
	return err
}

func (s *Service) Get(ctx context.Context, id int64) (RelocationPlan, error) {
	if err := s.Initialize(ctx); err != nil {
		return RelocationPlan{}, err
	}
	return s.readPlan(s.Store.DB().QueryRowContext(ctx, `SELECT id,run_id,status,summary,facts_digest,policies_json FROM relocation_plans WHERE id=?`, id))
}

func (s *Service) Latest(ctx context.Context, libraryID int64) (RelocationPlan, error) {
	if err := s.Initialize(ctx); err != nil {
		return RelocationPlan{}, err
	}
	return s.readPlan(s.Store.DB().QueryRowContext(ctx, `SELECT id,run_id,status,summary,facts_digest,policies_json FROM relocation_plans WHERE library_id=? ORDER BY id DESC LIMIT 1`, libraryID))
}

func (s *Service) readPlan(row *sql.Row) (RelocationPlan, error) {
	var plan RelocationPlan
	var id, runID int64
	var status, summary, digest, policies string
	if err := row.Scan(&id, &runID, &status, &summary, &digest, &policies); err != nil {
		return plan, err
	}
	if err := json.Unmarshal([]byte(summary), &plan); err != nil {
		return plan, err
	}
	plan.ID, plan.RunID, plan.Status, plan.FactsDigest, plan.PoliciesJSON = id, runID, status, digest, policies
	return plan, nil
}

func (s *Service) save(ctx context.Context, plan RelocationPlan) error {
	plan.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET status=?,summary=?,facts_digest=?,policies_json=? WHERE id=?`, plan.Status, body, plan.FactsDigest, plan.PoliciesJSON, plan.ID)
	return err
}

func (s *Service) add(ctx context.Context, plan *RelocationPlan, detail RelocationDetail) error {
	var ordinal int64
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM relocation_details WHERE plan_id=?`, plan.ID).Scan(&ordinal); err != nil {
		return err
	}
	detail.Ordinal = ordinal
	body, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if _, err = s.Store.DB().ExecContext(ctx, `INSERT INTO relocation_details(plan_id,ordinal,kind,detail) VALUES(?,?,?,?)`, plan.ID, ordinal, detail.Kind, body); err != nil {
		return err
	}
	switch detail.Kind {
	case "blocker":
		plan.BlockerCount++
	case "anomaly":
		plan.AnomalyCount++
		plan.AnomalyCounts[detail.Reason]++
	case "policy":
		plan.PolicyCount++
	case "mapping":
		if len(plan.Samples) < 6 {
			plan.Samples = append(plan.Samples, detail)
		}
	}
	return nil
}

func (s *Service) Details(ctx context.Context, id int64, kind string, offset, limit int) (RelocationDetails, error) {
	page := RelocationDetails{Items: []RelocationDetail{}, Offset: max(0, offset), Limit: min(100, max(1, limit))}
	if _, err := s.Get(ctx, id); err != nil {
		return page, err
	}
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM relocation_details WHERE plan_id=? AND (?='' OR kind=?)`, id, kind, kind).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT detail FROM relocation_details WHERE plan_id=? AND (?='' OR kind=?) ORDER BY ordinal LIMIT ? OFFSET ?`, id, kind, kind, page.Limit, page.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var detail RelocationDetail
		if err = rows.Scan(&body); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(body), &detail); err != nil {
			return page, err
		}
		page.Items = append(page.Items, detail)
	}
	return page, rows.Err()
}
