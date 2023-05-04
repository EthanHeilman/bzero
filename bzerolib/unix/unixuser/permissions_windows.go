//go:build windows

package unixuser

import (
	"fmt"

	"bastionzero.com/bzerolib/unix/filemode"
)

func (u *UnixUser) CanRead(path string) (bool, error) {
	return u.checkPermissions(path, filemode.Read)
}

func (u *UnixUser) CanWrite(path string) (bool, error) {
	return u.checkPermissions(path, filemode.Write)
}

func (u *UnixUser) CanExecute(path string) (bool, error) {
	return u.checkPermissions(path, filemode.Execute)
}

func (u *UnixUser) CanOpen(path string) (bool, error) {
	return u.checkPermissions(path, filemode.Open)
}

func (u *UnixUser) CanRemove(path string) (bool, error) {
	return u.checkPermissions(path, filemode.Remove)
}

// This function does some extra logic to determine whether a user can or cannot
// create a given file. It loops through the path, searching for the longest path
// that exists and then checks the user's ability to create in that directory.
func (u *UnixUser) CanCreate(path string) (bool, error) {
	return false, fmt.Errorf("operation not supported yet on windows")
}

func (u *UnixUser) checkPermissions(path string, check filemode.CheckType) (bool, error) {
		return false, fmt.Errorf("operation not supported yet on windows")
}
