package ssh

import (
	"bastionzero.com/bzerolib/bzio"
)

type IIdentityFile interface {
	SetKey(privateKey []byte) error
	GetKey() ([]byte, error)
	Path() string
}

type IdentityFile struct {
	filePath string
	fileIo   bzio.BzFileIo
}

func NewIdentityFile(filePath string, fileIo bzio.BzFileIo) *IdentityFile {
	return &IdentityFile{
		filePath: filePath,
		fileIo:   fileIo,
	}
}

func (f *IdentityFile) SetKey(privateKey []byte) error {
	return f.fileIo.WriteFile(f.filePath, privateKey, 0600)
}

func (f *IdentityFile) GetKey() ([]byte, error) {
	return f.fileIo.ReadFile(f.filePath)
}

func (f *IdentityFile) Path() string {
	return f.filePath
}
