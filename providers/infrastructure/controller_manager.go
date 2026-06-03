// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

// Platform controller manager — the one that reconciles Template CRs
// into per-template CRDs + backend setup. Lives alongside the legacy
// REST surface; the two coexist for PRs A-D and the REST handlers get
// deleted in PR E once the UI + MCP have migrated to the kcp-native
// path.
//
// The manager is OPT-IN via INFRASTRUCTURE_CONTROLLER_KUBECONFIG (or
// the standard KUBECONFIG fallback). When neither is set the provider
// runs as it does today: REST broker, no controller. That keeps the
// dev-mode/stub flow intact while the new code lands.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/faroshq/faros-kedge/providers/infrastructure/backend"
	"github.com/faroshq/faros-kedge/providers/infrastructure/backend/stub"
	"github.com/faroshq/faros-kedge/providers/infrastructure/controller/template"
	"github.com/faroshq/faros-kedge/providers/infrastructure/install"
)

// startControllerManager builds a controller-runtime manager pointed
// at the provider's own kcp workspace, installs the platform CRDs,
// registers the stub backend, and starts the Template controller.
// Returns errControllerDisabled when no kubeconfig is available — the
// caller treats that as "skip the manager, run REST-only".
func startControllerManager(ctx context.Context) error {
	config, err := loadControllerConfig()
	if err != nil {
		return err
	}

	// Install platform CRDs into this workspace. Idempotent. Doing
	// this before the manager starts means the Template type is
	// already registered when the controller's informer wakes up.
	if err := install.CRDs(ctx, config); err != nil {
		return fmt.Errorf("install CRDs: %w", err)
	}

	// Register the platform's own CRDs as APIExport resources so
	// tenants who APIBind see Templates from day one — without
	// waiting for an operator to apply the first Template. The
	// Template controller adds per-template resources at runtime
	// using the same machinery.
	if err := install.PlatformSchemaInAPIExport(ctx, config); err != nil {
		return fmt.Errorf("register platform schemas on APIExport: %w", err)
	}

	// CachedResource projection: makes Templates visible to tenants
	// in their own workspace via the kcp cache machinery. Without
	// this, tenants can see the per-template kinds (Redis, Postgres,
	// …) but not the Template catalog itself. Idempotent.
	if err := install.PlatformCachedResources(ctx, config); err != nil {
		return fmt.Errorf("install CachedResources: %w", err)
	}

	mgr, err := manager.New(config, manager.Options{
		// Disable the metrics server in PR A; the bind on :8080 would
		// collide with the provider's own HTTP server in dev. PR E
		// adds it back on a configurable port.
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return fmt.Errorf("manager.New: %w", err)
	}

	registry := backend.NewRegistry()
	if err := registry.Register(stub.New()); err != nil {
		return fmt.Errorf("register stub backend: %w", err)
	}
	// PR C: registry.Register(kro.New())

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("dynamic client: %w", err)
	}

	if err := (&template.Reconciler{
		Client:   mgr.GetClient(),
		Dynamic:  dyn,
		Backends: registry,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("template controller: %w", err)
	}

	go func() {
		log.Printf("infrastructure controller manager starting (backends=%v)", registry.Names())
		if err := mgr.Start(ctx); err != nil {
			log.Printf("controller manager exited: %v", err)
		}
	}()
	return nil
}

// loadControllerConfig returns a rest.Config for the workspace the
// platform controllers target. Looked up in this order:
//
//	INFRASTRUCTURE_CONTROLLER_KUBECONFIG  — provider-specific override
//	KUBECONFIG                            — standard env var
//	in-cluster service account            — when run as a pod
//
// Returns errControllerDisabled when none of the three resolve; the
// caller logs + continues without the controller.
func loadControllerConfig() (*rest.Config, error) {
	if p := os.Getenv("INFRASTRUCTURE_CONTROLLER_KUBECONFIG"); p != "" {
		c, err := clientcmd.BuildConfigFromFlags("", p)
		if err != nil {
			return nil, fmt.Errorf("INFRASTRUCTURE_CONTROLLER_KUBECONFIG: %w", err)
		}
		return c, nil
	}
	if p := os.Getenv("KUBECONFIG"); p != "" {
		c, err := clientcmd.BuildConfigFromFlags("", p)
		if err != nil {
			return nil, fmt.Errorf("KUBECONFIG: %w", err)
		}
		return c, nil
	}
	// In-cluster fallback. The error returned by InClusterConfig is
	// the right "not running in a pod" signal so we let it surface
	// up the chain as errControllerDisabled.
	c, err := rest.InClusterConfig()
	if err != nil {
		return nil, errControllerDisabled
	}
	return c, nil
}

// errControllerDisabled is the sentinel main() checks for so it can
// log + continue without the manager when no kubeconfig is in scope.
var errControllerDisabled = errors.New("no kubeconfig available; controller manager disabled")
