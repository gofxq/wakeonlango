package applog

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

type Options struct {
	FilePath string
	Prefix   string
}

func New(opts Options) (*log.Logger, func() error, error) {
	writers := []io.Writer{os.Stdout}
	closeFn := func() error { return nil }

	if opts.FilePath != "" {
		if dir := filepath.Dir(opts.FilePath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, nil, err
			}
		}

		file, err := os.OpenFile(opts.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		writers = append(writers, file)
		closeFn = file.Close
	}

	logger := log.New(io.MultiWriter(writers...), opts.Prefix, log.LstdFlags|log.Lmsgprefix)
	return logger, closeFn, nil
}
