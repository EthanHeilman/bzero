//go:build unix

package defaultshell

import (
	"fmt"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	bzshell "bastionzero.com/bzerolib/plugin/shell"
)

func (d *DefaultShell) start(attach bool) error {
	// Set initial terminal dimensions and then listen for any changes to
	// terminal size
	go d.listenForTerminalSizeChanges()

	// switch stdin into 'raw' mode
	// https://pkg.go.dev/golang.org/x/term#pkg-overview
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("error switching std to raw mode: %s", err)
	}

	d.tmb.Go(func() error {
		defer d.logger.Infof("closing action: %s", d.tmb.Err())
		defer term.Restore(int(os.Stdin.Fd()), oldState)

		go d.sendStdIn()
		return d.readFromStdIn()
	})

	return nil
}

func (d *DefaultShell) sendTerminalSize() {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err != nil {
		d.logger.Errorf("Failed to get current terminal size %s", err)
	} else {
		shellResizeMessage := bzshell.ShellResizeMessage{
			Rows: uint32(h),
			Cols: uint32(w),
		}
		d.sendOutputMessage(bzshell.ShellResize, shellResizeMessage)
	}
}

// Captures any terminal resize events using the SIGWINCH signal and send the
// new terminal size
func (d *DefaultShell) listenForTerminalSizeChanges() {
	// Send initial terminal size
	d.sendTerminalSize()

	ch := make(chan os.Signal, 1)
	sig := unix.SIGWINCH
	signal.Notify(ch, sig)

	for {
		select {
		case <-d.tmb.Dying():
			signal.Reset(sig)
			close(ch)
			return
		case <-ch:
			d.sendTerminalSize()
		}
	}
}
