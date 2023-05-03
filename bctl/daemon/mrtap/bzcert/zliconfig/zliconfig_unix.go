//go:build unix

package zliconfig

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func (z *ZLIConfig) load() error {
	if z.configPath == "" {
		return fmt.Errorf("no config path provided")
	}

	configFile, err := os.Open(z.configPath)
	if err != nil {
		return fmt.Errorf("could not open config file %s: %w", z.configPath, err)
	}
	defer configFile.Close()

	configFileBytes, err := io.ReadAll(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", z.configPath, err)
	}

	if err := json.Unmarshal(configFileBytes, z); err != nil {
		return fmt.Errorf("could not unmarshal config file %s: %w", z.configPath, err)
	}

	return nil
}
