package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	agentdata "bastionzero.com/agent/config/agentconfig/data"
	ksdata "bastionzero.com/agent/config/keyshardconfig/data"
	"bastionzero.com/bzerolib/filelock"
	"github.com/fsnotify/fsnotify"
)

const (
	// "Vault" was our old name for the config, renaming the .json file seemed unecessary at the time
	agentConfigFileName    = "vault.json"
	configFileLockName     = "vault.lock" // TODO: revisit
	keyShardConfigFileName = "keyshards.json"
)

// for Linux and Windows agents (i.e. excluding Kuberenetes)
type serverConfigClient struct {
	configPath string
	fileLock   *filelock.FileLock
	configType ConfigType

	// Used to check for changes between fetches and saves
	lastAgentMod    int64
	lastKeyShardMod int64
}

func NewServerConfigClient(configDir string, configType ConfigType) (*serverConfigClient, error) {
	var configPath string
	switch configType {
	case Agent:
		configPath = path.Join(configDir, agentConfigFileName)
	case KeyShard:
		configPath = path.Join(configDir, keyShardConfigFileName)
	default:
		return nil, fmt.Errorf("unsupported config type: %s", configType)
	}

	config := &serverConfigClient{
		configPath: configPath,
		configType: configType,
		fileLock:   filelock.NewFileLock(path.Join(configDir, configFileLockName)),
	}

	// check if file exists
	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) { // our file does not exist

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

	return config, nil
}

func (s *serverConfigClient) FetchAgentData() (agentdata.AgentDataV2, error) {
	var config agentdata.AgentDataV2

	if s.configType != Agent {
		return config, fmt.Errorf("cannot fetch agent data with %s client", s.configType)
	}

	lock, err := s.fileLock.AcquireLock()
	if err != nil {
		return config, err
	}
	defer lock.Unlock()

	file, err := os.ReadFile(s.configPath)
	if err != nil {
		return config, err
	}

	if info, err := os.Stat(s.configPath); err != nil {
		return config, fmt.Errorf("failed to get agent config file info %s: %w", s.configPath, err)
	} else {
		s.lastAgentMod = info.ModTime().UnixMilli()
	}

	if len(file) == 0 {
		return config, nil
	}

	if err := json.Unmarshal([]byte(file), &config); err != nil {
		return config, err
	}

	return config, nil
}

func (s *serverConfigClient) FetchKeyShardData() (ksdata.KeyShardData, error) {
	var config ksdata.KeyShardData

	if s.configType != KeyShard {
		return config, fmt.Errorf("cannot fetch key shard data with %s client", s.configType)
	}

	lock, err := s.fileLock.AcquireLock()
	if err != nil {
		return config, err
	}
	defer lock.Unlock()

	file, err := os.ReadFile(s.configPath)
	if err != nil {
		return config, err
	}

	if info, err := os.Stat(s.configPath); err != nil {
		return config, fmt.Errorf("failed to get key shard config file info %s: %w", s.configPath, err)
	} else {
		s.lastKeyShardMod = info.ModTime().UnixMilli()
	}

	if len(file) == 0 {
		return config, nil
	}

	if err := json.Unmarshal([]byte(file), &config); err != nil {
		return config, err
	}

	return config, nil
}

func (s *serverConfigClient) Save(d interface{}) error {
	// grab our file lock so we're not accidentally writing at the same time
	// as other processes which is possible during registration
	lock, err := s.fileLock.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Unlock()

	// first check if our config has been changed since we last fetched so that we're
	// 1000% sure we will not be overwriting anything
	if info, err := os.Stat(s.configPath); err != nil {
		return fmt.Errorf("failed to get config file info %s: %w", s.configPath, err)
	} else if (s.configType == Agent && s.lastAgentMod != info.ModTime().UnixMilli()) ||
		(s.configType == KeyShard && s.lastKeyShardMod != info.ModTime().UnixMilli()) {
		return fmt.Errorf("config has changed since it was last fetched: our last mod")
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
	if err := os.WriteFile(s.configPath, dataBytes, 0644); err != nil {
		return err
	}

	return nil
}

func (s *serverConfigClient) WaitForRegistration(ctx context.Context) error {
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
						if data, err := s.FetchAgentData(); err == nil && !data.PublicKey.IsEmpty() {
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
