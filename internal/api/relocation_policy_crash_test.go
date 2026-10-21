package api

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
)

func TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries(t *testing.T) {
	for _, phase := range []string{"config_rename", "runtime_apply", "derived_effects"} {
		t.Run(phase, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			target := filepath.Join(root, "target")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			cfg := c.currentConfig()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
			cfg.LLM.APIKey = "test-secret-must-not-enter-journal"
			if err := c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			plan := startRelocationPreview(t, c, lib.ID, target)
			c.Close()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestRelocationPolicyCrashProcess$")
			cmd.Env = append(os.Environ(), "RELOCATION_CRASH_PHASE="+phase, "RELOCATION_CRASH_CONFIG="+c.configPath, "RELOCATION_CRASH_PLAN="+strconv.FormatInt(plan.ID, 10))
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("crash process did not reach %s: %v %s", phase, err, output)
			}
			disk, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			// A later unrelated edit must survive recovery from either decision.

			if err = config.NewManager(disk).Save(c.configPath, nil, func(next *config.Config) error { next.Tasks.RunSlots = 9; return nil }, nil, nil); err != nil {
				t.Fatal(err)
			}
			disk, err = config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			fresh := config.NewManager(disk)
			reopened := newControllerCore(store, scanner.NewScanner(store, fresh), fresh, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(reopened.Close)
			var id int64
			if err = store.DB().QueryRow(`SELECT id FROM relocation_operations WHERE plan_id=?`, plan.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			op := awaitRelocationOperation(t, reopened, id)
			got, _ := store.GetBook(context.Background(), book.ID)
			wantPath, wantRoot, wantStatus := filepath.Join(target, "series", "book.cbz"), target, "completed"
			if phase == "config_rename" {
				wantPath, wantRoot, wantStatus = book.Path, lib.Path, "interrupted"
			}
			if got.Path != wantPath || op.Status != wantStatus {
				t.Fatalf("fresh recovery: %+v book=%s", op, got.Path)
			}
			live := reopened.currentConfig()
			actual, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if live.Tasks.RunSlots != 9 || actual.Tasks.RunSlots != 9 || config.ResolveStoragePolicy(live, wantRoot).StorageProfile != "hdd_external" || config.ResolveStoragePolicy(*actual, wantRoot).StorageProfile != "hdd_external" {
				t.Fatal("recovery lost policy or unrelated settings")
			}
			var patch string
			if err = store.DB().QueryRow(`SELECT patch FROM relocation_policy_journal WHERE operation_id=?`, id).Scan(&patch); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(patch, cfg.LLM.APIKey) {
				t.Fatal("journal contains unrelated secret")
			}
			reopened.Close()
			secondCfg, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			secondManager := config.NewManager(secondCfg)
			second := newControllerCore(store, scanner.NewScanner(store, secondManager), secondManager, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(second.Close)
			if result := awaitRelocationOperation(t, second, id); result.Status != wantStatus {
				t.Fatalf("repeated recovery: %+v", result)
			}
		})
	}
}

func TestRelocationPolicyCrashProcess(t *testing.T) {
	phase := os.Getenv("RELOCATION_CRASH_PHASE")
	if phase == "" {
		t.Skip("subprocess fixture")
	}
	path := os.Getenv("RELOCATION_CRASH_CONFIG")
	cfg, err := config.LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer := config.FileWriter(func(path string, data []byte, mode os.FileMode) error {
		if err := config.AtomicWriteFile(path, data, mode); err != nil {
			return err
		}
		if phase == "config_rename" {
			os.Exit(23)
		}
		return nil
	})
	mgr := config.NewManager(cfg, writer)
	c := newControllerCore(store, scanner.NewScanner(store, mgr), mgr, path, defaultControllerCacheSizes())
	defer c.Close()
	if phase == "runtime_apply" {
		c.relocation.ApplyConfig = func(cfg *config.Config) error {
			if err := runtimecfg.Apply(cfg); err != nil {
				return err
			}
			os.Exit(23)
			return nil
		}
	}
	if phase == "derived_effects" {
		effects := c.relocation.Effects
		c.relocation.Effects = func(ctx context.Context, op RelocationOperation) error {
			if err := effects(ctx, op); err != nil {
				return err
			}
			os.Exit(23)
			return nil
		}
	}
	planID, err := strconv.ParseInt(os.Getenv("RELOCATION_CRASH_PLAN"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	submitRelocation(t, c, planID)
	select {
	case <-time.After(20 * time.Second):
		t.Fatal("durability boundary not reached")
	}
}
