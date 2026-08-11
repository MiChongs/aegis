package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"go.uber.org/fx"
)

type RuntimeSystem struct {
	app     *fx.App
	errors  <-chan error
	API     *APIApp
	Worker  *WorkerApp
	Unified *UnifiedApp
}

type runtimeMode string

const (
	runtimeModeAPI     runtimeMode = "api"
	runtimeModeWorker  runtimeMode = "worker"
	runtimeModeUnified runtimeMode = "unified"
)

type runtimeErrorReporter struct {
	ch   chan error
	once sync.Once
}

func newRuntimeErrorReporter() *runtimeErrorReporter {
	return &runtimeErrorReporter{ch: make(chan error, 1)}
}

func (r *runtimeErrorReporter) report(err error) {
	if r == nil || err == nil {
		return
	}
	r.once.Do(func() {
		r.ch <- err
	})
}

func NewAPISystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeAPI, ctx, cl)
}

func NewWorkerSystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeWorker, ctx, cl)
}

func NewUnifiedSystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeUnified, ctx, cl)
}

func (s *RuntimeSystem) Start(ctx context.Context) error {
	if s == nil || s.app == nil {
		return fmt.Errorf("runtime system is not initialized")
	}
	return s.app.Start(ctx)
}

func (s *RuntimeSystem) Stop(ctx context.Context) error {
	if s == nil || s.app == nil {
		return nil
	}
	return s.app.Stop(ctx)
}

func (s *RuntimeSystem) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("runtime system is not initialized")
	}
	startCtx, cancelStart := context.WithTimeout(ctx, s.shutdownTimeout())
	defer cancelStart()
	if err := s.Start(startCtx); err != nil {
		return err
	}

	defer func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), s.shutdownTimeout())
		defer cancelStop()
		_ = s.Stop(stopCtx)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-s.errors:
		return err
	}
}

func (s *RuntimeSystem) shutdownTimeout() time.Duration {
	switch {
	case s.API != nil && s.API.Config.ShutdownTimeout > 0:
		return s.API.Config.ShutdownTimeout
	case s.Worker != nil && s.Worker.Config.ShutdownTimeout > 0:
		return s.Worker.Config.ShutdownTimeout
	default:
		return 30 * time.Second
	}
}

func newRuntimeSystem(mode runtimeMode, constructCtx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	if constructCtx == nil {
		constructCtx = context.Background()
	}

	system := &RuntimeSystem{}
	reporter := newRuntimeErrorReporter()

	options := []fx.Option{
		fx.Supply(cl),
		fx.Supply(reporter),
		fx.Provide(func() (*config.Manager, error) {
			return config.NewManager()
		}),
	}

	switch mode {
	case runtimeModeAPI:
		options = append(options,
			fx.Provide(func(manager *config.Manager, crash *crashlog.Logger) (*APIApp, error) {
				return NewAPIAppWithConfigManager(constructCtx, crash, manager)
			}),
			fx.Invoke(registerAPIRuntimeLifecycle),
			fx.Populate(&system.API),
		)
	case runtimeModeWorker:
		options = append(options,
			fx.Provide(func(manager *config.Manager, crash *crashlog.Logger) (*WorkerApp, error) {
				return NewWorkerAppWithConfigManager(constructCtx, crash, manager)
			}),
			fx.Invoke(registerWorkerRuntimeLifecycle),
			fx.Populate(&system.Worker),
		)
	case runtimeModeUnified:
		options = append(options,
			fx.Provide(
				func(manager *config.Manager, crash *crashlog.Logger) (*APIApp, error) {
					return NewAPIAppWithConfigManager(constructCtx, crash, manager)
				},
				func(manager *config.Manager, crash *crashlog.Logger) (*WorkerApp, error) {
					return NewWorkerAppWithConfigManager(constructCtx, crash, manager)
				},
				func(api *APIApp, worker *WorkerApp) *UnifiedApp {
					return &UnifiedApp{
						API:    api,
						Worker: worker,
					}
				},
			),
			fx.Invoke(registerAPIRuntimeLifecycle, registerWorkerRuntimeLifecycle),
			fx.Populate(&system.API, &system.Worker, &system.Unified),
		)
	default:
		return nil, fmt.Errorf("unsupported runtime mode %q", mode)
	}

	app := fx.New(options...)
	if err := app.Err(); err != nil {
		return nil, err
	}

	system.app = app
	system.errors = reporter.ch
	return system, nil
}

func registerAPIRuntimeLifecycle(lc fx.Lifecycle, shutdowner fx.Shutdowner, reporter *runtimeErrorReporter, app *APIApp) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runCtx, runCancel := context.WithCancel(context.Background())
			cancel = runCancel

			if err := app.Start(runCtx); err != nil {
				cancel()
				cancel = nil
				return err
			}

			go func() {
				if err := app.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					reporter.report(fmt.Errorf("api serve failed: %w", err))
					_ = shutdowner.Shutdown()
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			app.Close(ctx)
			return nil
		},
	})
}

func registerWorkerRuntimeLifecycle(lc fx.Lifecycle, shutdowner fx.Shutdowner, reporter *runtimeErrorReporter, worker *WorkerApp) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runCtx, runCancel := context.WithCancel(context.Background())
			cancel = runCancel

			go func() {
				if err := worker.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
					reporter.report(fmt.Errorf("worker run failed: %w", err))
					_ = shutdowner.Shutdown()
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			worker.Close(ctx)
			return nil
		},
	})
}
