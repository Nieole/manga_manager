package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

type Service struct {
	Store            database.Store
	Config           *config.Manager
	Files            FileSystem
	Acquire          func(context.Context, int64) (func(), error)
	AcquireOperation func(context.Context, RelocationOperation) (func(), error)
	Effects          func(context.Context, RelocationOperation) error
	access           commitAccess
	transient        bool
	evaluated        RelocationPlan
	db               database.DBTX
	once             sync.Once
	err              error
}

func (s *Service) dbtx() database.DBTX {
	if s.db != nil {
		return s.db
	}
	return s.Store.DB()
}

func (s *Service) libraryPaths(ctx context.Context) ([]struct {
	ID   int64
	Path string
}, error) {
	rows, err := s.dbtx().QueryContext(ctx, `SELECT id,path FROM libraries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []struct {
		ID   int64
		Path string
	}
	for rows.Next() {
		var item struct {
			ID   int64
			Path string
		}
		if err = rows.Scan(&item.ID, &item.Path); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func NewService(store database.Store, cfg *config.Manager) *Service {
	return &Service{Store: store, Config: cfg, Files: HostFiles{}, access: commitAccess{releases: map[int64]func(){}}}
}

// Initialize is idempotent for existing databases. Plans deliberately have no
// cascading dependency on run history: pruning runs must preserve their facts.
// database.Migrate must have initialized taskstore before this domain schema.
func (s *Service) Initialize(ctx context.Context) error {
	if s.transient {
		return nil
	}
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
CREATE INDEX IF NOT EXISTS idx_relocation_details_kind ON relocation_details(plan_id,kind,ordinal);
CREATE TABLE IF NOT EXISTS relocation_operations (
 id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER NOT NULL UNIQUE,
 library_id INTEGER NOT NULL, run_id INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL, result TEXT NOT NULL
);
CREATE TRIGGER IF NOT EXISTS relocation_commit_terminal
AFTER UPDATE OF status ON runs
WHEN NEW.status IN ('cancelled','failed','interrupted')
 AND EXISTS (SELECT 1 FROM tasks WHERE id=NEW.task_id AND type='relocation_commit' AND scope='library')
BEGIN
 UPDATE relocation_operations SET run_id=NEW.id
 WHERE run_id=0 AND library_id=(SELECT scope_id FROM tasks WHERE id=NEW.task_id)
  AND EXISTS (SELECT 1 FROM run_labels WHERE run_id=NEW.id AND key='operation_id' AND value=CAST(relocation_operations.id AS TEXT));
 UPDATE relocation_operations SET status=NEW.status
 WHERE status='pending' AND run_id=NEW.id;
END;
CREATE TRIGGER IF NOT EXISTS relocation_check_terminal
AFTER UPDATE OF status ON runs
WHEN NEW.status IN ('completed','cancelled','failed','interrupted')
 AND EXISTS (SELECT 1 FROM tasks WHERE id=NEW.task_id AND type='relocation_check' AND scope='library')
BEGIN
 UPDATE relocation_plans SET run_id=NEW.id
 WHERE run_id=0 AND library_id=(SELECT scope_id FROM tasks WHERE id=NEW.task_id)
  AND EXISTS (SELECT 1 FROM run_labels WHERE run_id=NEW.id AND key='plan_id' AND value=CAST(relocation_plans.id AS TEXT));
 UPDATE relocation_plans
 SET status=CASE WHEN NEW.status='completed' THEN 'failed' ELSE NEW.status END
 WHERE status IN ('pending','checking')
  AND library_id=(SELECT scope_id FROM tasks WHERE id=NEW.task_id)
  AND run_id=NEW.id;
END;`)
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
	// Admission writes labels under its mutex before publication or execution.
	// The trigger can therefore bind a terminal check even before start returns,
	// and history deletion cannot erase that fact. A failed label write followed
	// by a missing unbound run is a persistence failure, not an inferred terminal.
	result, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET run_id=?, status=
 CASE WHEN status IN ('pending','checking') THEN COALESCE((
  SELECT CASE WHEN status='completed' THEN 'failed' ELSE status END FROM runs
  WHERE id=? AND status IN ('completed','cancelled','failed','interrupted')
 ),status) ELSE status END
 WHERE id=? AND (run_id=0 OR run_id=?) AND (run_id=? OR EXISTS (
  SELECT 1 FROM runs JOIN tasks ON tasks.id=runs.task_id
  WHERE runs.id=? AND tasks.type='relocation_check' AND tasks.scope='library'
   AND tasks.scope_id=relocation_plans.library_id
 ))`, runID, runID, id, runID, runID, runID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("relocation run binding unavailable")
	}
	return nil
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
	return s.readPlan(s.dbtx().QueryRowContext(ctx, `SELECT id,run_id,status,summary,facts_digest,policies_json FROM relocation_plans WHERE id=?`, id))
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
	if !s.transient {
		var protectionErr error
		plan.Protection, protectionErr = s.Store.LibraryRelocationProtection(context.Background(), plan.LibraryID)
		if protectionErr != nil {
			return plan, protectionErr
		}
		err := s.Store.DB().QueryRow(`SELECT id,run_id FROM relocation_operations WHERE plan_id=?`, id).Scan(&plan.OperationID, &plan.CommitRunID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return plan, err
		}
	}
	return plan, nil
}

func (s *Service) save(ctx context.Context, plan RelocationPlan) error {
	if s.transient {
		s.evaluated = plan
		return nil
	}
	plan.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = s.Store.DB().ExecContext(ctx, `UPDATE relocation_plans SET status=?,summary=?,facts_digest=?,policies_json=? WHERE id=?`, plan.Status, body, plan.FactsDigest, plan.PoliciesJSON, plan.ID)
	return err
}

func (s *Service) add(ctx context.Context, plan *RelocationPlan, detail RelocationDetail) error {
	if s.transient {
		countDetail(plan, detail)
		return nil
	}
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
	countDetail(plan, detail)
	return nil
}

func countDetail(plan *RelocationPlan, detail RelocationDetail) {
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
