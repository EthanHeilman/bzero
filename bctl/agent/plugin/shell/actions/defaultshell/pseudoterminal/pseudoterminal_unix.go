//go:build unix

// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may not
// use this file except in compliance with the License. A copy of the
// License is located at
//
// http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// This code has been modified from the code covered by the Apache License 2.0.
// Modifications Copyright (C) 2022 BastionZero Inc.  The BastionZero Agent
// is licensed under the Apache 2.0 License.

package pseudoterminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/unix/unixuser"
	"github.com/creack/pty"
)

const (
	termEnvVariable     = "TERM=xterm-256color"
	langEnvVariable     = "LANG=C.UTF-8"
	langEnvVariableKey  = "LANG"
	defaultShellCommand = "sh"
	homeEnvVariableName = "HOME="
)

type PseudoTerminal struct {
	logger   *logger.Logger
	ptyFile  *os.File
	command  *exec.Cmd
	doneChan chan struct{}
}

// New starts pty and provides handles to stdin and stdout
func New(logger *logger.Logger, runAsUser *unixuser.UnixUser, commandstr string) (*PseudoTerminal, error) {
	logger.Info("Starting up pseudo terminal")

	// Attempt to get default shell to use for the runAsUser
	var shellCommand string
	if runAsUser.Shell != "" {
		shellCommand = runAsUser.Shell
		logger.Infof("Found preferred shell for %s: %s", runAsUser.Username, shellCommand)
	} else {
		logger.Infof("%s does not have a preferred shell, falling back on default: %s", runAsUser.Username, defaultShellCommand)
		shellCommand = defaultShellCommand
	}

	if cmd, err := buildCommand(runAsUser, commandstr, shellCommand); err != nil {
		return nil, err
	} else if ptyFile, err := pty.Start(cmd); err != nil {
		return nil, fmt.Errorf("failed to start pty: %s", err)
	} else {

		doneChan := make(chan struct{})

		go func() {
			defer close(doneChan)

			if err := cmd.Wait(); err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode := exitError.ExitCode()
					logger.Errorf("pty command exited with non-zero exit code %d err: %s", exitCode, err)
				} else {
					logger.Errorf("pty command exited with unknown exit code")
				}
			}
		}()

		return &PseudoTerminal{
			logger:   logger,
			ptyFile:  ptyFile,
			command:  cmd,
			doneChan: doneChan,
		}, nil
	}
}

func buildCommand(runAsUser *unixuser.UnixUser, customCommand string, shellCommand string) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	if strings.TrimSpace(customCommand) == "" {
		// if customCommand not provided then default to launching an interactive login shell

		// Add --login option to shell command so that this is a login shell and
		// will source shell profile dot files automatically
		// https://unix.stackexchange.com/a/46856
		cmd = exec.Command(shellCommand, "-l")
	} else {
		// else if customCommand is provided then run the command in a shell with the -c option
		commandArgs := []string{"-c", customCommand}
		cmd = exec.Command(shellCommand, commandArgs...)
	}

	// TERM is set as linux by pty which has an issue where vi editor screen does not get cleared.
	// Setting TERM as xterm-256color as used by standard terminals to fix this issue
	cmd.Env = append(os.Environ(), termEnvVariable)

	// If LANG environment variable is not set, shell defaults to POSIX which can contain 256 single-byte characters.
	// Setting C.UTF-8 as default LANG environment variable as Session Manager supports UTF-8 encoding only.
	langEnvVariableValue := os.Getenv(langEnvVariableKey)
	if langEnvVariableValue == "" {
		cmd.Env = append(cmd.Env, langEnvVariable)
	}

	gids, err := runAsUser.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve users group ids: %s", err)
	}

	usr, err := unixuser.Current()
	if err != nil {
		return nil, err
	}

	// Only set groups if agent is running as root and this is a linux machine
	isRootOnLinux := usr.Uid == 0 && runtime.GOOS == "linux"

	// run command as user
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    runAsUser.Uid,
			Gid:    runAsUser.Gid,
			Groups: gids,

			// Setting supplementary group IDs is a privileged action only the root user can do.
			// if this is set to true, users may have to use sudo to run commands they should have access to
			NoSetGroups: !isRootOnLinux,
		},
	}

	// Setting home environment variable for RunAs user
	cmd.Env = append(cmd.Env, homeEnvVariableName+runAsUser.HomeDir)

	// Setting cwd of the command to be the user's home directory
	cmd.Dir = runAsUser.HomeDir

	return cmd, nil
}

func (p *PseudoTerminal) Done() <-chan struct{} {
	return p.doneChan
}

func (p *PseudoTerminal) Kill() {
	// close the ptyFile so we can no longer read/write from stdio
	if p.ptyFile != nil {
		p.logger.Infof("closing pty file")
		if err := p.ptyFile.Close(); err != nil {
			p.logger.Errorf("failed to close pty: %s", err)
		}
		p.ptyFile = nil
	}

	// Also kill the pty command process so the cmd.Wait() will return and the
	// done channel will get closed
	if p.command.Process != nil {
		p.logger.Infof("killing pty command process")
		if err := p.command.Process.Kill(); err != nil {
			p.logger.Errorf("failed to kill pty command process: %s", err)
		}
	}
}

func (p *PseudoTerminal) StdIn() io.Writer {
	return p.ptyFile
}

func (p *PseudoTerminal) StdOut() io.Reader {
	return p.ptyFile
}

// SetSize sets size of console terminal window.
func (p *PseudoTerminal) SetSize(cols, rows uint32) error {
	winSize := pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}

	if err := pty.Setsize(p.ptyFile, &winSize); err != nil {
		return fmt.Errorf("set terminal window size failed: %s", err)
	}
	return nil
}
