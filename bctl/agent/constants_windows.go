//go:build windows

package main

import (
	"os"
	"path/filepath"
)

const (
	// BastionZeroFolder is the path under local app data.
	BastionZeroFolder = "BastionZero\\Agent"
)

func initConstants() {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = filepath.Join(os.Getenv("AllUsersProfile"), "Application Data")
	}
	bzDataPath := filepath.Join(programData, BastionZeroFolder)
	configDir = filepath.Join(bzDataPath, "RuntimeConfig")
	defaultLogPath = filepath.Join(bzDataPath, "Logs", "bzero-agent.log")
}
