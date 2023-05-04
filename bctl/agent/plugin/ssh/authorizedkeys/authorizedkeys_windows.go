//go:build windows

package authorizedkeys

import (
	"fmt"
	"time"

	"bastionzero.com/bzerolib/filelock"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/unix/unixuser"
)

const (
	authorizedKeyComment         = "bzero-temp-key"
	lockFileName                 = ".bzero.lock"
	authorizedKeyFileName        = "authorized_keys"
	authorizedKeysFilePermission = 0600 // only owner (user) can read/write
	authorizedKeysDirPermission  = 0700 // only owner (user) can read/read/execute
)

type IAuthorizedKeys interface {
	Add(pubkey string) error
}

type AuthorizedKeys struct {
	logger   *logger.Logger
	doneChan chan struct{}

	keyLifetime time.Duration

	usr *unixuser.UnixUser

	keyFilePath string
	fileLock    *filelock.FileLock
}

func New(
	logger *logger.Logger,
	doneChan chan struct{},
	usr *unixuser.UnixUser,
	authKeyFolder string,
	lockFileFolder string,
	keyLifetime time.Duration,
) (*AuthorizedKeys, error) {

	return nil, fmt.Errorf("operation not supported yet on windows")
}

func (a *AuthorizedKeys) Add(pubkey string) error {
	return fmt.Errorf("operation not supported yet on windows")
}
