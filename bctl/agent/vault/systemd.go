package vault

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"github.com/fsnotify/fsnotify"
	"github.com/gofrs/flock"
)

const (
	DefaultVaultDirectory = "/etc/bzero"
	vaultFileName         = "vault.json"
	vaultFileLockName     = "vault.lock"
)

type SystemDVault struct {
	vaultPath string
	data      vault
	vaultLock sync.RWMutex
	fileLock  *flock.Flock
}

func LoadSystemDVault(vaultDir string) (*SystemDVault, error) {
	vaultPath := path.Join(vaultDir, vaultFileName)
	fileLock := filelock.NewFileLock(path.Join(vaultDir, vaultFileLockName))

	// check if file exists
	if f, err := os.Stat(vaultPath); os.IsNotExist(err) { // our file does not exist

		// create our directory, if it doesn't exit
		if err := os.MkdirAll(vaultDir, os.ModePerm); err != nil {
			return nil, err
		}

		// create our file
		if _, err := os.Create(vaultPath); err != nil {
			return nil, err
		} else {
			fileLockLock, err := fileLock.NewLock()
			if err != nil {
				return nil, err
			}

			vault := SystemDVault{
				vaultPath: vaultPath,
				fileLock:  fileLockLock,
			}
			vault.save()

			// return our newly created, and empty vault
			return &vault, nil
		}
	} else if err != nil {
		return nil, err
	} else if f.Size() == 0 { // our file exists, but is empty
		fileLockLock, err := fileLock.NewLock()
		if err != nil {
			return nil, err
		}

		vault := SystemDVault{
			vaultPath: vaultPath,
			fileLock:  fileLockLock,
		}
		vault.save()

		// return our newly created, and empty vault
		return &vault, nil
	}

	fileLockLock, err := fileLock.NewLock()
	if err != nil {
		return nil, err
	}

	sysDVault := SystemDVault{
		vaultPath: vaultPath,
		fileLock:  fileLockLock,
	}

	// if the file does exist, read it into memory
	if v, err := sysDVault.fetchVault(); err != nil {
		return nil, err
	} else {
		sysDVault.data = v
		return &sysDVault, nil
	}
}

// We know the vault exists, we just need to load it
func (s *SystemDVault) fetchVault() (vault, error) {
	var config vault
	for {
		if acquiredLock, err := s.fileLock.TryLock(); err != nil {
			return config, fmt.Errorf("error acquiring lock: %w", err)
		} else if acquiredLock {
			break
		}
	}
	defer s.fileLock.Unlock()

	if file, err := ioutil.ReadFile(s.vaultPath); err != nil {
		return config, err
	} else if err := json.Unmarshal([]byte(file), &config); err != nil {
		return config, err
	} else {
		return config, err
	}
}

func (s *SystemDVault) Reload() error {
	if vault, err := s.fetchVault(); err != nil {
		return err
	} else {
		s.data = vault
		return nil
	}
}

func (s *SystemDVault) GetPublicKey() *keypair.PublicKey {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return &s.data.PublicKey
}

func (s *SystemDVault) GetPrivateKey() *keypair.PrivateKey {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return &s.data.PrivateKey
}

func (s *SystemDVault) GetIdpOrgId() string {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return s.data.IdpOrgId
}

func (s *SystemDVault) GetIdpProvider() string {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return s.data.IdpProvider
}

func (s *SystemDVault) GetAgentIdentityToken() string {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return s.data.AgentIdentityToken
}

func (s *SystemDVault) GetShutdownInfo() (string, map[string]string) {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return s.data.ShutdownReason, s.data.ShutdownState
}

func (s *SystemDVault) GetTargetId() string {
	s.vaultLock.RLock()
	defer s.vaultLock.RUnlock()

	return s.data.TargetId
}

func (s *SystemDVault) SetVersion(version string) error {
	s.vaultLock.Lock()
	defer s.vaultLock.Unlock()

	currentVault, err := s.fetchVault()
	if err != nil {
		return fmt.Errorf("failed to load vault: %w", err)
	}

	// If our private keys are mismatched, it means a new registration
	// has happened and we shouldn't write anything
	if !(&s.data.PrivateKey).Equals(currentVault.PrivateKey) {
		return fmt.Errorf("new registration detected, reload vault")
	}

	currentVault.Version = version

	s.data = currentVault
	return s.save()
}

func (s *SystemDVault) SetShutdownInfo(reason string, state map[string]string) error {
	s.vaultLock.Lock()
	defer s.vaultLock.Unlock()

	currentVault, err := s.fetchVault()
	if err != nil {
		return fmt.Errorf("failed to load vault: %w", err)
	}

	currentVault.ShutdownReason = reason
	currentVault.ShutdownState = state

	s.data = currentVault
	return s.save()
}

func (s *SystemDVault) SetAgentIdentityToken(token string) error {
	s.vaultLock.Lock()
	defer s.vaultLock.Unlock()

	currentVault, err := s.fetchVault()
	if err != nil {
		return fmt.Errorf("failed to load vault: %w", err)
	}

	// If our private keys are mismatched, it means a new registration
	// has happened and we shouldn't write anything
	if !(&s.data.PrivateKey).Equals(currentVault.PrivateKey) {
		return fmt.Errorf("new registration detected, reload vault")
	}

	currentVault.AgentIdentityToken = token

	s.data = currentVault
	return s.save()
}

func (s *SystemDVault) save() error {
	// grab our file lock so we're not accidentally writing at the same time
	// as other processes which is possible during registration
	for {
		if acquiredLock, err := s.fileLock.TryLock(); err == nil {
			return fmt.Errorf("error acquiring lock: %w", err)
		} else if acquiredLock {
			break
		}
	}
	defer s.fileLock.Unlock()

	// overwrite entire file every time
	dataBytes, err := json.Marshal(s.data)
	if err != nil {
		return err
	}

	// empty out our file
	if err := os.Truncate(s.vaultPath, 0); err != nil {
		return err
	}

	// replace it with our new vault
	if err := ioutil.WriteFile(s.vaultPath, dataBytes, 0644); err != nil {
		return err
	}

	return nil
}

func (s *SystemDVault) WaitForRegistration(cancel <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("error starting new file watcher: %w", err)
	}
	defer watcher.Close()

	done := make(chan error)
	go func() {
		for {
			select {
			case <-cancel:
				done <- nil
				return
			case event, ok := <-watcher.Events:
				if !ok {
					done <- fmt.Errorf("file watcher closed events channel")
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					var config vault
					if file, err := ioutil.ReadFile(s.vaultPath); err != nil {
						continue
					} else if err := json.Unmarshal([]byte(file), &config); err != nil {
						continue
					} else {
						// if we haven't completed registration yet, continue waiting
						if (&config.PublicKey).IsEmpty() {
							continue
						} else {
							done <- nil
							return
						}
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					done <- fmt.Errorf("file watcher closed errors channel")
				}
				done <- fmt.Errorf("file watcher caught error: %s", err)
			}
		}
	}()

	if err := watcher.Add(s.vaultPath); err != nil {
		return fmt.Errorf("unable to watch vault file %s: %w", s.vaultPath, err)
	}

	return <-done
}
