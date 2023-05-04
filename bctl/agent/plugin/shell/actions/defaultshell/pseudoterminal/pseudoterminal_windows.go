//go:build windows

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

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/unix/unixuser"
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
	return nil, fmt.Errorf("operation not supported yet on windows")
}

func (p *PseudoTerminal) Done() <-chan struct{} {
	return p.doneChan
}

func (p *PseudoTerminal) Kill() {
}

func (p *PseudoTerminal) StdIn() io.Writer {
	return p.ptyFile
}

func (p *PseudoTerminal) StdOut() io.Reader {
	return p.ptyFile
}

// SetSize sets size of console terminal window.
func (p *PseudoTerminal) SetSize(cols, rows uint32) error {
	return fmt.Errorf("operation not supported yet on windows")
}
