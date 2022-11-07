package bzio

import (
	"io/fs"
	"os"
)

// an interface providing ways to interact with files
// can be implemented by native os methods or dummy functions for testing
type BzFileIo interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Truncate(name string, size int64) error
}

// the default implementation
type OsFileIo struct{}

func (f OsFileIo) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (f OsFileIo) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (f OsFileIo) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (f OsFileIo) Truncate(name string, size int64) error {
	return os.Truncate(name, size)
}
