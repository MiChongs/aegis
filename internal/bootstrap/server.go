package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type UnifiedApp struct {
	API    *APIApp
	Worker *WorkerApp
}

func NewUnifiedApp(ctx context.Context) (*UnifiedApp, error) {
	api, err := NewAPIApp(ctx)
	if err != nil {
		return nil, err
	}

	worker, err := NewWorkerApp(ctx)
	if err != nil {
		api.Close(context.Background())
		return nil, err
	}

	return &UnifiedApp{
		API:    api,
		Worker: worker,
	}, nil
}

func (u *UnifiedApp) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		if err := u.Worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("worker run failed: %w", err)
		}
	}()

	go func() {
		if err := u.API.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("api serve failed: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (u *UnifiedApp) Close(ctx context.Context) {
	if u.Worker != nil {
		u.Worker.Close(ctx)
	}
	if u.API != nil {
		u.API.Close(ctx)
	}
}
