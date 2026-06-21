/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	sdkinstall "github.com/faroshq/provider-sdk/install"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/faroshq/provider-sandbox/controller/devenvironment"
	"github.com/faroshq/provider-sandbox/install"
	sandboxscheme "github.com/faroshq/provider-sandbox/scheme"
)

var errControllerDisabled = errors.New("no kubeconfig available; controller manager disabled")

func startControllerManager(ctx context.Context, providerConfig, runtimeConfig *rest.Config) error {
	if providerConfig == nil || runtimeConfig == nil {
		return errControllerDisabled
	}
	ctrl.SetLogger(klog.NewKlogr())
	scheme := sandboxscheme.NewScheme()
	workspacePath := os.Getenv("SANDBOX_WORKSPACE_PATH")
	if workspacePath == "" {
		workspacePath = install.DefaultWorkspacePath
	}
	dyn, err := dynamic.NewForConfig(providerConfig)
	if err != nil {
		return fmt.Errorf("dynamic provider client: %w", err)
	}
	if err := sdkinstall.EnsureAPIExportEndpointSlice(ctx, dyn, install.APIExportEndpointSliceName, install.APIExportName, workspacePath); err != nil {
		log.Printf("controller manager: WARNING could not ensure APIExportEndpointSlice: %v", err)
	}
	provider, err := apiexport.New(providerConfig, install.APIExportEndpointSliceName, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating apiexport multicluster provider: %w", err)
	}
	mgr, err := mcmanager.New(providerConfig, provider, manager.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return fmt.Errorf("creating multicluster manager: %w", err)
	}
	runtimeClient, err := kubernetes.NewForConfig(runtimeConfig)
	if err != nil {
		return fmt.Errorf("runtime kubernetes client: %w", err)
	}
	if err := (&devenvironment.Reconciler{RuntimeClient: runtimeClient}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("DevEnvironment controller: %w", err)
	}
	go func() {
		log.Printf("sandbox controller manager starting")
		if err := mgr.Start(ctx); err != nil {
			log.Printf("controller manager exited: %v", err)
		}
	}()
	return nil
}
