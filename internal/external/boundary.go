package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrPathUnresolvable means the physical boundary cannot safely be established.
var ErrPathUnresolvable = errors.New("cannot resolve external-library path boundary")

// resolveBoundary resolves existing ancestors too, so an offline registered root
// still protects its directory beneath a symlinked parent. Permission failures fail closed.
func resolveBoundary(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrPathUnresolvable
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		if runtime.GOOS == "windows" {
			if strings.HasPrefix(strings.ToLower(resolved), `\\?\unc\`) {
				resolved = `\\` + resolved[8:]
			} else {
				resolved = strings.TrimPrefix(resolved, `\\?\`)
			}
		}
		return resolved, nil
	}
	if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %w", ErrPathUnresolvable, err)
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %w", ErrPathUnresolvable, err)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", fmt.Errorf("%w: %w", ErrPathUnresolvable, err)
	}
	base, err := resolveBoundary(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(path)), nil
}

func (m *Manager) validateExternalRoot(ctx context.Context, path string) (string, error) {
	resolved, err := resolveBoundary(path)
	if err != nil {
		return "", err
	}
	libraries, err := m.store.ListLibraries(ctx)
	if err != nil {
		return "", err
	}
	for _, lib := range libraries {
		if pathsOverlapLiteral(path, lib.Path) {
			return "", ErrExternalPathInsideLibrary
		}
		root, err := resolveBoundary(lib.Path)
		if err != nil {
			return "", err
		}
		if pathsOverlapLiteral(resolved, root) {
			return "", ErrExternalPathInsideLibrary
		}
	}
	return resolved, nil
}

// ValidateSession rechecks live registered roots and physical paths before a retained
// session or plan is used. Unsafe or changed sessions are invalidated, not replanned.
func (m *Manager) ValidateSession(ctx context.Context, libraryID int64, sessionID string) error {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	if !ok || s.LibraryID != libraryID {
		m.mu.RUnlock()
		return ErrSessionNotFound
	}
	snapshot := *s
	m.mu.RUnlock()
	lib, err := m.store.GetLibrary(ctx, libraryID)
	if err == nil && lib.Path != snapshot.LibraryPath {
		err = ErrSessionNotFound
	}
	var physical string
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(snapshot.ExternalPath)
		if err == nil && !info.IsDir() {
			err = ErrExternalPathInvalid
		}
	}
	if err == nil {
		physical, err = m.validateExternalRoot(ctx, snapshot.ExternalPath)
	}
	if err == nil && !sameBoundary(physical, snapshot.ExternalResolved) {
		err = ErrSessionNotFound
	}
	var owner string
	if err == nil {
		owner, err = resolveBoundary(lib.Path)
	}
	if err == nil && !sameBoundary(owner, snapshot.LibraryResolved) {
		err = ErrSessionNotFound
	}
	if err != nil {
		m.ClearSession(libraryID, sessionID)
	}
	return err
}

func sameBoundary(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// ValidateOperation protects a prepared destination against new roots and symlink
// changes between planning and execution, while preserving the original plan.
func (m *Manager) ValidateOperation(ctx context.Context, libraryID int64, sessionID string, op TransferOperation) error {
	if err := m.ValidateSession(ctx, libraryID, sessionID); err != nil {
		return err
	}
	snapshot, err := m.GetSession(libraryID, sessionID)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(snapshot.ExternalPath, op.Destination)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrExternalPathInvalid
	}
	_, err = m.validateExternalRoot(ctx, op.Destination)
	return err
}
