//go:build windows

package zliconfig

import (
	"encoding/json"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const (
	winRegKeyPath = `Software\BastionZero` // formattable string to access creds
)

func (z *ZLIConfig) load() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, winRegKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("failed to access registry: %w", err)
	}
	defer k.Close()

	mrtap, _, err := k.GetStringValue("mrtap")
	if err != nil || len(mrtap) == 0 {
		return fmt.Errorf("failed to retrieve Windows Registry value associated with key %s: %w", winRegKeyPath, err)
	}

	if err := json.Unmarshal([]byte(mrtap), &z.CertConfig); err != nil {
		return fmt.Errorf("malformed BastionZero credentials entry: %w", err)
	}

	tokenSet, _, err := k.GetStringValue("tokenSet")
	if err != nil || len(tokenSet) == 0 {
		return fmt.Errorf("failed to retrieve Windows Registry value associated with key %s: %w", winRegKeyPath, err)
	}

	if err := json.Unmarshal([]byte(tokenSet), &z.TokenSet); err != nil {
		return fmt.Errorf("malformed BastionZero credentials entry: %w", err)
	}

	return nil
}
