package relocation

import (
	"context"
	"errors"
	"reflect"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
)

// RestorationRequest names a server-created check and the administrator's decision.
type RestorationRequest struct {
	PlanID         int64  `json:"plan_id"`
	ConfirmRelease bool   `json:"confirm_release"`
	AcceptLossRisk bool   `json:"accept_loss_risk"`
	ScanMode       string `json:"scan_mode"`
	ScanInterval   int64  `json:"scan_interval"`
	ScanFormats    string `json:"scan_formats"`
}

func (s *Service) CreateRestoration(ctx context.Context, libraryID int64) (RelocationPlan, error) {
	protection, err := s.Store.LibraryRelocationProtection(ctx, libraryID)
	if err != nil {
		return RelocationPlan{}, err
	}
	if protection == nil {
		return RelocationPlan{}, ErrPlanNotReady
	}
	lib, err := s.Store.GetLibrary(ctx, libraryID)
	if err != nil {
		return RelocationPlan{}, err
	}
	plan, err := s.Create(ctx, libraryID, lib.Path, lib.Path)
	if err != nil {
		return plan, err
	}
	plan.Purpose, plan.ProtectionID = "restoration", protection.OperationID
	return plan, s.save(ctx, plan)
}

func (s *Service) PrepareRestoration(ctx context.Context, request RestorationRequest) (RelocationOperation, bool, error) {
	if !request.ConfirmRelease {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	if request.ScanMode != "none" && request.ScanMode != "watch" && request.ScanMode != "interval" {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	if request.ScanInterval <= 0 || len(config.ParseScanFormats(request.ScanFormats)) == 0 {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	request.ScanFormats = config.NormalizeScanFormatsCSV(request.ScanFormats)
	return s.prepare(ctx, request.PlanID, &request, request.AcceptLossRisk)
}

func (s *Service) commitRestoration(ctx context.Context, op RelocationOperation, handle *runhandle.Handle, committed func(RelocationOperation)) error {
	release, err := s.acquireOperation(ctx, op)
	if err != nil {
		op.Status, op.Code = "failed", "relocation_conflict"
		_ = s.saveOperation(context.WithoutCancel(ctx), op)
		return err
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()
	err = s.withRestorationConfig(func(cfg config.Config, verify func() error) error {
		initial := &Service{Store: s.Store, persistence: s.persistence, Config: config.NewManager(&cfg), Files: s.Files, transient: true}
		plan, e := initial.Get(ctx, op.PlanID)
		if e != nil {
			return e
		}
		if e = initial.Check(ctx, plan.ID, handle, nil); e != nil {
			return e
		}
		if initial.evaluated.FactsDigest != plan.FactsDigest || initial.evaluated.BlockerCount != 0 {
			return ErrStalePlan
		}
		if e = handle.DisableCancellation(); e != nil {
			return e
		}
		if e = s.persistRestorationCheck(ctx, op, cfg, handle, verify); e != nil {
			return e
		}
		op.Status = "paths_committed"
		if committed != nil {
			committed(op)
		}
		keep = true
		s.access.mu.Lock()
		s.access.releases[op.ID] = release
		s.access.mu.Unlock()
		return s.finishRestoration(context.WithoutCancel(ctx), op, verify)
	})
	if err != nil {
		persisted, e := s.Operation(context.WithoutCancel(ctx), op.ID)
		if e == nil && persisted.Status == "paths_committed" {
			persisted.Code = "restoration_recovering"
			_ = s.saveOperation(context.WithoutCancel(ctx), persisted)
			return err
		}
		op.Status, op.Code = "failed", "restoration_failed"
		if errors.Is(err, context.Canceled) {
			op.Status, op.Code = "cancelled", "restoration_not_committed"
		}
		if errors.Is(err, ErrStalePlan) {
			op.Code = "relocation_stale_plan"
		}
		_ = s.saveOperation(context.WithoutCancel(ctx), op)
	}
	return err
}

func (s *Service) withRestorationConfig(fn func(config.Config, func() error) error) error {
	return s.withCommitConfig(func(cfg config.Config, session *config.UpdateSession) error {
		verify := func() error { return nil }
		if session != nil {
			disk, err := session.Read()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(policyFacts(disk), policyFacts(cfg)) {
				return ErrStalePlan
			}
			verify = session.Verify
		}
		return fn(cfg, verify)
	})
}

func (s *Service) finishRestoration(ctx context.Context, op RelocationOperation, verify ...func() error) error {
	if s.RestoreEffects == nil {
		return errors.New("restoration effects unavailable")
	}
	if err := s.RestoreEffects(ctx, op); err != nil {
		return err
	}
	if len(verify) > 0 {
		if err := verify[0](); err != nil {
			return err
		}
	}
	if err := s.releaseRestoration(ctx, op); err != nil {
		return err
	}
	s.releaseOperation(op.ID)
	return nil
}
