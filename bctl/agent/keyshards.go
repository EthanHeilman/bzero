package main

import (
	"context"
	"fmt"

	"bastionzero.com/bctl/v1/bctl/agent/config"
	"bastionzero.com/bctl/v1/bctl/agent/config/client"
)

// TODO: key shard handler functions

func getKeyShardConfig() (*config.KeyShardConfig, error) {
	var keyShardConfig *config.KeyShardConfig
	switch getAgentType() {
	case Kubernetes:
		if keyShardClient, err := client.NewKubernetesClient(context.Background(), namespace, targetName, client.KeyShard); err != nil {
			return nil, fmt.Errorf("failed to initialize kube key shard config client: %w", err)
		} else if keyShardConfig, err = config.LoadKeyShardConfig(keyShardClient); err != nil {
			return nil, fmt.Errorf("failed to load key shard config: %w", err)
		}
	case Systemd:
		if keyShardClient, err := client.NewSystemdClient(configDir, client.KeyShard); err != nil {
			return nil, fmt.Errorf("failed to initialize systemd key shard config client: %w", err)
		} else if keyShardConfig, err = config.LoadKeyShardConfig(keyShardClient); err != nil {
			return nil, fmt.Errorf("failed to load key shard config: %w", err)
		}
	}

	return keyShardConfig, nil
}

func printKeyShardConfig() {
	ks, err := getKeyShardConfig()
	if err != nil {
		fmt.Println("TODO: we messed up: %s", err)
	}

	ks.
}
