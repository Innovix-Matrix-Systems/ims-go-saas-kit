package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

// RunHTTP serves the handler until the context is cancelled, then drains
// connections within the configured shutdown timeout.
func RunHTTP(ctx context.Context, cfg *config.Config, logger *slog.Logger, handler http.Handler) error {
	server := &http.Server{
		Addr:         cfg.HTTP.ListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening",
			"addr", cfg.HTTP.ListenAddr,
			"env", cfg.App.Env,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections",
			"timeout", cfg.HTTP.ShutdownTimeout.String(),
		)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		logger.Info("http server stopped")
		return nil
	}
}
