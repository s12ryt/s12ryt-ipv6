package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type ProductionService interface {
	Run(context.Context) error
}

type ProductionOptions struct {
	DataDirectory string
	Stdout        io.Writer
}

type ProductionDependencies struct {
	AcquireLock func(string) (io.Closer, error)
	Build       func(ProductionOptions) (ProductionService, error)
}

func RunProduction(ctx context.Context, options ProductionOptions, dependencies ProductionDependencies) (err error) {
	if ctx == nil {
		return errors.New("production context is required")
	}
	if strings.TrimSpace(options.DataDirectory) == "" {
		return errors.New("production data directory is required")
	}
	if options.Stdout == nil {
		return errors.New("production stdout is required")
	}
	if dependencies.AcquireLock == nil || dependencies.Build == nil {
		return errors.New("production dependencies are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	options.DataDirectory = filepath.Clean(options.DataDirectory)
	lock, err := dependencies.AcquireLock(filepath.Join(options.DataDirectory, "service.lock"))
	if err != nil {
		return fmt.Errorf("acquire service lock: %w", err)
	}
	if lock == nil {
		return errors.New("service lock is nil")
	}
	defer func() {
		err = errors.Join(err, lock.Close())
	}()

	service, err := dependencies.Build(options)
	if err != nil {
		return fmt.Errorf("build production service: %w", err)
	}
	if service == nil {
		return errors.New("production service is nil")
	}
	if err := service.Run(ctx); err != nil {
		return fmt.Errorf("run production service: %w", err)
	}
	return nil
}
