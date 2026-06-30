// Command LifeRouter starts the standalone pathfinding service.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kentalives/LifeRouter/embedded"
	"github.com/Kentalives/LifeRouter/internal/app"
	"github.com/Kentalives/LifeRouter/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[PATHFINDING] FATAL: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadConfig(nil)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// The executable entrypoint reuses the embedded dependency wiring before
	// handing control to the service dispatcher, so for normal use, a custom
	// ExternalSystem should be used.
	dep, err := embedded.DefaultDependencies(ctx, cfg)
	if err != nil {
		return fmt.Errorf("hooking default dependencies: %w", err)
	}

	disp, err := app.Run(ctx, cfg, dep)
	if err != nil {
		return fmt.Errorf("running service: %w", err)
	}

	<-ctx.Done()

	disp.Shutdown()
	return nil
}
