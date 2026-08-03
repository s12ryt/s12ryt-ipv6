package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type ServiceOptions struct {
	ListenManagement   func(context.Context) ([]net.Listener, error)
	InitializePassword func() (string, bool, error)
	InitializeRuntime  func(context.Context) error
	ReconcileResources func(context.Context) error
	RestoreNodes       func(context.Context) error
	ServeHTTP          func(context.Context, []net.Listener) error
	ServeControl       func(context.Context) error
	RunNAT64           func(context.Context) error
	ShutdownNodes      func(context.Context) error
	ShutdownNetwork    func(context.Context) error
	ShutdownFirewall   func(context.Context) error
	SaveStatistics     func() error
	CloseLog           func() error
	ReportDegraded     func(error)
	PasswordOutput     io.Writer
	StatsInterval      time.Duration
}

type Service struct {
	options ServiceOptions
}

func NewService(options ServiceOptions) (*Service, error) {
	required := []struct {
		name string
		set  bool
	}{
		{"management listener", options.ListenManagement != nil},
		{"password initializer", options.InitializePassword != nil},
		{"runtime initializer", options.InitializeRuntime != nil},
		{"resource reconciler", options.ReconcileResources != nil},
		{"node restorer", options.RestoreNodes != nil},
		{"HTTP server", options.ServeHTTP != nil},
		{"control server", options.ServeControl != nil},
		{"NAT64 monitor", options.RunNAT64 != nil},
		{"node shutdown", options.ShutdownNodes != nil},
		{"network shutdown", options.ShutdownNetwork != nil},
		{"firewall shutdown", options.ShutdownFirewall != nil},
		{"statistics saver", options.SaveStatistics != nil},
		{"log closer", options.CloseLog != nil},
		{"password output", options.PasswordOutput != nil},
	}
	for _, dependency := range required {
		if !dependency.set {
			return nil, fmt.Errorf("%s is required", dependency.name)
		}
	}
	if options.StatsInterval <= 0 {
		return nil, errors.New("statistics interval must be positive")
	}
	if options.ReportDegraded == nil {
		options.ReportDegraded = func(error) {}
	}
	return &Service{options: options}, nil
}

func (s *Service) Run(ctx context.Context) error {
	listeners, err := s.options.ListenManagement(ctx)
	if err != nil {
		return fmt.Errorf("listen for management HTTP: %w", err)
	}
	if len(listeners) == 0 {
		return errors.New("management listener returned no sockets")
	}
	defer closeListeners(listeners)

	password, created, err := s.options.InitializePassword()
	if err != nil {
		return fmt.Errorf("initialize administrator password: %w", err)
	}
	if created {
		if _, err := fmt.Fprintf(s.options.PasswordOutput, "initial admin password: %s\n", password); err != nil {
			return fmt.Errorf("print initial administrator password: %w", err)
		}
	}
	if err := s.options.InitializeRuntime(ctx); err != nil {
		cleanupErr := s.options.ShutdownFirewall(context.WithoutCancel(ctx))
		return errors.Join(
			fmt.Errorf("initialize runtime: %w", err),
			wrapLifecycleError("shutdown firewall", cleanupErr),
		)
	}
	if err := s.options.ReconcileResources(ctx); err != nil {
		s.options.ReportDegraded(fmt.Errorf("reconcile resources: %w", err))
	}
	if err := s.options.RestoreNodes(ctx); err != nil {
		s.options.ReportDegraded(fmt.Errorf("restore nodes: %w", err))
	}

	runCtx, cancel := context.WithCancel(ctx)
	type componentResult struct {
		name string
		err  error
	}
	results := make(chan componentResult, 3)
	var components sync.WaitGroup
	start := func(name string, run func(context.Context) error) {
		components.Add(1)
		go func() {
			defer components.Done()
			results <- componentResult{name: name, err: run(runCtx)}
		}()
	}
	start("HTTP server", func(componentCtx context.Context) error {
		return s.options.ServeHTTP(componentCtx, listeners)
	})
	start("control server", s.options.ServeControl)
	start("NAT64 monitor", s.options.RunNAT64)

	ticker := time.NewTicker(s.options.StatsInterval)
	defer ticker.Stop()
	var runErr error
	waiting := true
	for waiting {
		select {
		case <-ctx.Done():
			waiting = false
		case result := <-results:
			if ctx.Err() == nil {
				if result.err == nil {
					runErr = fmt.Errorf("%s stopped unexpectedly", result.name)
				} else {
					runErr = fmt.Errorf("%s stopped: %w", result.name, result.err)
				}
			}
			waiting = false
		case <-ticker.C:
			if err := s.options.SaveStatistics(); err != nil {
				s.options.ReportDegraded(fmt.Errorf("save statistics: %w", err))
			}
		}
	}

	cancel()
	closeListeners(listeners)
	components.Wait()

	cleanupCtx := context.WithoutCancel(ctx)
	cleanupErr := errors.Join(
		wrapLifecycleError("shutdown nodes", s.options.ShutdownNodes(cleanupCtx)),
		wrapLifecycleError("shutdown network", s.options.ShutdownNetwork(cleanupCtx)),
		wrapLifecycleError("shutdown firewall", s.options.ShutdownFirewall(cleanupCtx)),
		wrapLifecycleError("save statistics", s.options.SaveStatistics()),
		wrapLifecycleError("close log", s.options.CloseLog()),
	)
	return errors.Join(runErr, cleanupErr)
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		if listener != nil {
			_ = listener.Close()
		}
	}
}

func wrapLifecycleError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
