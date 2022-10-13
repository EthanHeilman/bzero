package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"bastionzero.com/bctl/v1/bctl/agent/rbac"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"gopkg.in/tomb.v2"
)

type KubernetesAgent struct {
	tmb    tomb.Tomb
	config *vault.KubernetesVault
	logger *logger.Logger
}

func NewKubeAgent(version string) (*KubernetesAgent, error) {
	// Load our vault
	config, err := vault.LoadKubernetesVault(context.Background(), namespace, targetName)
	if err != nil {
		return nil, err
	}

	// Make sure our agent version is correct
	if err := config.SetVersion(version); err != nil {
		return nil, err
	}

	// Create our logger
	log, err := logger.New(&logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
	})
	if err != nil {
		return nil, err
	}
	log.AddAgentVersion(version)
	log.AddAgentType("kubernetes")

	return &KubernetesAgent{
		logger: log,
		config: config,
	}, nil
}

func (k *KubernetesAgent) Run() error {
	// Verify we have the correct RBAC permissions
	if err := rbac.CheckPermissions(k.logger, namespace); err != nil {
		return fmt.Errorf("error verifying agent kubernetes setup: %s", err)
	} else {
		k.logger.Info("Namespace and service account permissions verified")
	}

	return nil
}
