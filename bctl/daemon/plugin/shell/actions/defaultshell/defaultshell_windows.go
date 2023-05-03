//go:build windows

package defaultshell

import (
	"fmt"
	"time"

	"github.com/containerd/console"

	bzshell "bastionzero.com/bzerolib/plugin/shell"
)

func (d *DefaultShell) start(attach bool) error {
	current := console.Current()

	d.tmb.Go(func() error {
		defer d.logger.Infof("closing action: %s", d.tmb.Err())
		defer current.Reset()

		d.tmb.Go(func() error { return d.listenForTerminalSizeChanges(current) })

		// switch our terminal into 'raw' mode
		if err := current.SetRaw(); err != nil {
			return fmt.Errorf("error switching terminal to raw mode: %s", err)
		}

		go d.sendStdIn()
		return d.readFromStdIn()
	})

	return nil
}

func (d *DefaultShell) listenForTerminalSizeChanges(console console.Console) error {
	// Send our initial size
	oldSize, err := console.Size()
	if err != nil {
		return fmt.Errorf("failed to get current terminal size %s", err)
	}

	shellResizeMessage := bzshell.ShellResizeMessage{
		Rows: uint32(oldSize.Height),
		Cols: uint32(oldSize.Width),
	}
	d.sendOutputMessage(bzshell.ShellResize, shellResizeMessage)

	// Occasionally polls the terminal size to check for changes and sends any it detects
	for {
		select {
		case <-d.tmb.Dying():
			return nil
		case <-time.After(50 * time.Millisecond):
			if newSize, err := console.Size(); err != nil {
				d.logger.Errorf("Failed to get current terminal size %s", err)
			} else if newSize.Width != oldSize.Width || newSize.Height != oldSize.Height {

				shellResizeMessage := bzshell.ShellResizeMessage{
					Rows: uint32(newSize.Height),
					Cols: uint32(newSize.Width),
				}
				d.sendOutputMessage(bzshell.ShellResize, shellResizeMessage)

				oldSize = newSize
			}
		}
	}
}
