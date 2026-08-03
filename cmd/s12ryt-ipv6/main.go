package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/app"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

var version = "dev"

const defaultDataDirectory = "/etc/s12ryt-ipv6"

type commandDependencies struct {
	serve         func(context.Context, string, io.Writer) error
	resetPassword func(context.Context, string) (string, error)
}

type noOpSessionRevoker struct{}

func (noOpSessionRevoker) Revoke() {}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr, defaultCommandDependencies()))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDependencies(args, stdout, stderr, defaultCommandDependencies())
}

func runWithDependencies(args []string, stdout, stderr io.Writer, dependencies commandDependencies) int {
	return runWithContext(context.Background(), args, stdout, stderr, dependencies)
}

func runWithContext(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies commandDependencies) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "s12ryt-ipv6 %s\n", version)
		return 0
	}
	if len(args) == 0 || args[0] == "serve" {
		serveArgs := args
		if len(serveArgs) > 0 {
			serveArgs = serveArgs[1:]
		}
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		dataDirectory := flags.String("data-dir", defaultDataDirectory, "configuration and state directory")
		if err := flags.Parse(serveArgs); err != nil || flags.NArg() != 0 || strings.TrimSpace(*dataDirectory) == "" {
			printUsage(stderr)
			return 2
		}
		if dependencies.serve == nil || dependencies.serve(ctx, *dataDirectory, stdout) != nil {
			fmt.Fprintln(stderr, "service failed")
			return 1
		}
		return 0
	}
	if len(args) >= 2 && args[0] == "admin" && args[1] == "reset-password" {
		flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
		flags.SetOutput(stderr)
		dataDirectory := flags.String("data-dir", defaultDataDirectory, "configuration and state directory")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(*dataDirectory) == "" {
			printUsage(stderr)
			return 2
		}
		if dependencies.resetPassword == nil {
			fmt.Fprintln(stderr, "admin password reset failed")
			return 1
		}
		password, err := dependencies.resetPassword(context.Background(), *dataDirectory)
		if err != nil {
			fmt.Fprintln(stderr, "admin password reset failed")
			return 1
		}
		fmt.Fprintf(stdout, "new admin password: %s\n", password)
		return 0
	}

	printUsage(stderr)
	return 2
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "usage: s12ryt-ipv6 [serve] [--data-dir PATH] | s12ryt-ipv6 version | s12ryt-ipv6 admin reset-password [--data-dir PATH]")
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{serve: serveProduction, resetPassword: resetAdminPassword}
}

func serveProduction(ctx context.Context, dataDirectory string, stdout io.Writer) error {
	return app.RunProduction(ctx, app.ProductionOptions{DataDirectory: dataDirectory, Stdout: stdout}, app.ProductionDependencies{
		AcquireLock: admin.AcquireServiceLock,
		Build:       app.BuildProduction,
	})
}

func resetAdminPassword(ctx context.Context, dataDirectory string) (string, error) {
	client, err := admin.NewControlClient(admin.ControlClientOptions{Dial: admin.DialControlSocket, Timeout: 5 * time.Second})
	if err != nil {
		return "", err
	}
	store, err := admin.NewFilePasswordStore(filepath.Join(dataDirectory, "admin-password.yaml"))
	if err != nil {
		return "", err
	}
	manager, err := admin.NewPasswordManager(store, secret.DefaultPasswordHasher(), noOpSessionRevoker{}, nil)
	if err != nil {
		return "", err
	}
	workflow, err := admin.NewPasswordResetWorkflow(admin.PasswordResetWorkflowOptions{
		Control: client, Direct: manager, AcquireLock: admin.AcquireServiceLock,
		ControlPath: filepath.Join(dataDirectory, "control.sock"),
		LockPath:    filepath.Join(dataDirectory, "service.lock"),
	})
	if err != nil {
		return "", err
	}
	return workflow.Reset(ctx, "")
}
