package logger

import (
	"io"
)

func MockLogger(writer io.Writer) *Logger {
	config := &Config{
		ConsoleWriters: []io.Writer{writer},
	}

	if logger, err := New(config); err == nil {
		return logger
	}
	return nil
}
