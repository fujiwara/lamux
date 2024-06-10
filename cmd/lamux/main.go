package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	app "github.com/fujiwara/lamux"
	"golang.org/x/sys/unix"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, unix.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	return app.Run(ctx)
}
