package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"github.com/fsnotify/fsnotify"
	"github.com/gofrs/flock"
)

const (
	// "Vault" was our old name for the config, renaming the .json file seemed unecessary at the time
	configFileName     = "vault.json"
	configFileLockName = "vault.lock"
)

type SystemdClient struct {
	configPath string
	fileLock   *flock.Flock

	// Used to check for changes between fetches and saves
	lastMod int64
}

func NewSystemdClient(configDir string) (*SystemdClient, error) {
	configPath := path.Join(configDir, configFileName)
	fileLock := filelock.NewFileLock(path.Join(configDir, configFileLockName))

	config := &SystemdClient{
		configPath: configPath,
	}

	// check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) { // our file does not exist

		// create our directory, if it doesn't exit
		if err := os.MkdirAll(configDir, os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create config directory %s: %w", configDir, err)
		}

		// create our file
		if _, err := os.Create(configPath); err != nil {
			return nil, fmt.Errorf("failed to create config file %s: %w", configPath, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get file system information on our config %s: %w", configPath, err)
	}

	fileLockLock, err := fileLock.NewLock()
	if err != nil {
		return nil, err
	}

	config.fileLock = fileLockLock
	return config, nil
}

func (s *SystemdClient) Fetch() (data.DataV2, error) {
	var config data.DataV2
	for {
		if acquiredLock, err := s.fileLock.TryLock(); err != nil {
			return config, fmt.Errorf("error acquiring lock: %w", err)
		} else if acquiredLock {
			break
		}
	}
	defer s.fileLock.Unlock()

	file, err := ioutil.ReadFile(s.configPath)
	if err != nil {
		return config, err
	}

	if info, err := os.Stat(s.configPath); err != nil {
		return config, fmt.Errorf("failed to get config file info %s: %w", s.configPath, err)
	} else {
		s.lastMod = info.ModTime().UnixMilli()
	}

	if len(file) == 0 {
		return config, nil
	}

	if err := json.Unmarshal([]byte(file), &config); err != nil {
		return config, err
	}

	return config, nil
}

func (s *SystemdClient) Save(d data.DataV2) error {
	// grab our file lock so we're not accidentally writing at the same time
	// as other processes which is possible during registration
	for {
		if acquiredLock, err := s.fileLock.TryLock(); err != nil {
			return fmt.Errorf("error acquiring lock: %w", err)
		} else if acquiredLock {
			break
		}
	}
	defer s.fileLock.Unlock()

	// first check if our config has been changed since we last fetched so that we're
	// 1000% sure we will not be overwriting anything
	if info, err := os.Stat(s.configPath); err != nil {
		return fmt.Errorf("failed to get config file info %s: %w", s.configPath, err)
	} else if s.lastMod != info.ModTime().UnixMilli() {
		return fmt.Errorf("config has changed since it was last fetched")
	}

	// empty out our file
	if err := os.Truncate(s.configPath, 0); err != nil {
		return err
	}

	// overwrite entire file every time
	dataBytes, err := json.Marshal(d)
	if err != nil {
		return err
	}

	// replace it with our new config
	if err := ioutil.WriteFile(s.configPath, dataBytes, 0644); err != nil {
		return err
	}

	return nil
}

func (s *SystemdClient) WaitForRegistration(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("error starting new file watcher: %w", err)
	}
	defer watcher.Close()

	done := make(chan error)
	go func() {
		done <- func() error {
			for {
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled")
				case event, ok := <-watcher.Events:
					if !ok {
						return fmt.Errorf("file watcher closed events channel")
					}

					if event.Op&fsnotify.Write == fsnotify.Write {
						if data, err := s.Fetch(); err == nil && !data.PublicKey.IsEmpty() {
							return nil
						}
					}
				case err, ok := <-watcher.Errors:
					if !ok {
						return fmt.Errorf("file watcher closed errors channel")
					}
					return fmt.Errorf("file watcher caught error: %w", err)
				}
			}
		}()
	}()

	if err := watcher.Add(s.configPath); err != nil {
		return fmt.Errorf("unable to watch config file %s: %w", s.configPath, err)
	}

	return <-done
}
