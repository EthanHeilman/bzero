package main

/*
Functions supporting the `keyShards` subcommand
*/

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"bastionzero.com/bctl/v1/bctl/agent/config"
	"bastionzero.com/bctl/v1/bctl/agent/config/client"
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
)

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
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	data, err := json.Marshal(ks)
	if err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(data), "", "    "); err != nil {
		fmt.Printf("%s\n", data)
		return
	}

	fmt.Printf("%s\n", prettyJSON.String())
}

func clearKeyShardConfig() {
	ks, err := getKeyShardConfig()
	if err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	err = ks.Clear()
	var noopError *config.NoOpError
	if errors.As(err, &noopError) {
		fmt.Println("Agent's keyshard configuration is already empty")
		return
	} else if err != nil {
		fmt.Printf("error: failed to clear keyshard config: %s\n", err)
		return
	}

	fmt.Println("Successfully cleared agent's keyshard configuration")
}

func addKeyShardData(path string) {
	rawData, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("failed to read data from file: %s\n", err)
		return
	}

	var ksData data.KeyShardData
	if err = json.Unmarshal(rawData, &ksData); err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	ks, err := getKeyShardConfig()
	if err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	for _, ksEntry := range ksData.Keys {
		if err = ks.AddKey(ksEntry); err != nil {
			fmt.Printf("failed to add key: %s\n", err)
			return
		}
	}

	fmt.Println("Successfully added keys to agent's keyshard configuration")
}

func addTargetIds(targetIds []string) {
	ks, err := getKeyShardConfig()
	if err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	for _, targetId := range targetIds {
		if err = ks.AddTarget(targetId); err != nil {
			fmt.Printf("failed to add target: %s\n", err)
			return
		}
	}

	fmt.Println("Successfully added targets to agent's keyshard configuration")
}

func removeTargetIds(targetIds []string) {
	ks, err := getKeyShardConfig()
	if err != nil {
		fmt.Printf("error: failed to load keyshard config: %s\n", err)
		return
	}

	for _, targetId := range targetIds {
		if err = ks.DeleteTarget(targetId, true); err != nil {
			fmt.Printf("failed to remove target: %s\n", err)
			return
		}
	}

	fmt.Println("Successfully removed targets from agent's keyshard configuration")
}
