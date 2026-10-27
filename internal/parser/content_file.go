package parser

import (
	"os"
	"sync"
)

// ContentFile is an original book stream visible to the shared archive borrow registry.
// Close is idempotent; reaching EOF does not release the borrow.
type ContentFile struct {
	file    *os.File
	once    sync.Once
	err     error
	release func()
}

// OpenContentFile opens original bytes without decoding the archive format.
func OpenContentFile(path string) (*ContentFile, error) {
	release := retainArchiveBorrow(path)
	file, err := os.Open(path)
	if err != nil {
		release()
		return nil, err
	}
	return &ContentFile{file: file, release: release}, nil
}

func (f *ContentFile) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *ContentFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}
func (f *ContentFile) Stat() (os.FileInfo, error) { return f.file.Stat() }
func (f *ContentFile) Close() error {
	f.once.Do(func() { f.err = f.file.Close(); f.release() })
	return f.err
}
