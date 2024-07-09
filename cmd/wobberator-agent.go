package main

import (
	"context"
	"github.com/wobcom/wobberator/internal/app"
	config2 "github.com/wobcom/wobberator/internal/pkg/config"
	"github.com/wobcom/wobberator/internal/pkg/kubernetes"
	"log/slog"
	"os"
	"sync"
)

func main() {

	slog.SetLogLoggerLevel(slog.LevelDebug)

	clientset, err := kubernetes.GetClient()
	if err != nil {
		slog.Error("Kubernetes client could not be loaded", "err", err)
		os.Exit(1)
	}

	config, err := config2.GetConfig()
	if err != nil {
		slog.Error("Config could not be loaded", "err", err)
		os.Exit(1)
	}
	ctx := context.Background()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("Calling RunHostRouteAssignment")
		app.RunHostRouteAssignment(ctx, clientset, &config.HostRouteAssignmentConfig)
	}()

	wg.Wait()
}
