package bookcontent

import (
	"context"
	"fmt"
	"os"
	"sync"

	"manga-manager/internal/parser"
)

// FileError identifies a storage failure for a resolved book source.
type FileError struct {
	Source Source
	Err    error
}

func (e *FileError) Error() string { return e.Err.Error() }
func (e *FileError) Unwrap() error { return e.Err }

// ReadHandle retains original bytes and library access until Close, including after EOF.
// Read and Seek follow os.File semantics; Close is idempotent.
type ReadHandle struct {
	Source  Source
	Info    os.FileInfo
	file    *parser.ContentFile
	release func()
	once    sync.Once
	err     error
}

func (h *ReadHandle) Read(p []byte) (int, error) { return h.file.Read(p) }
func (h *ReadHandle) Seek(offset int64, whence int) (int64, error) {
	return h.file.Seek(offset, whence)
}
func (h *ReadHandle) Close() error {
	h.once.Do(func() { h.err = h.file.Close(); h.release() })
	return h.err
}

// Open resolves a current source and retains existing library admission and the shared
// archive borrow until the caller closes the returned stream. It does not decode content.
func (s *Service) Open(ctx context.Context, id int64, access LibraryAccess) (*ReadHandle, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		version := access.LibraryAccessVersion()
		source, err := s.lookup(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSourceMissing, err)
		}
		release, err := access.AcquireLibraryRead(source.LibraryID)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBusy, err)
		}
		// A relocation may have completed between lookup and admission.
		if version != access.LibraryAccessVersion() {
			release()
			continue
		}
		file, err := parser.OpenContentFile(source.Path)
		if err != nil {
			release()
			return nil, &FileError{Source: source, Err: err}
		}
		info, err := file.Stat()
		if err == nil && info.IsDir() {
			err = fmt.Errorf("book path is a directory: %s", source.Path)
		}
		if err != nil {
			_ = file.Close()
			release()
			return nil, &FileError{Source: source, Err: err}
		}
		return &ReadHandle{Source: source, Info: info, file: file, release: release}, nil
	}
}
