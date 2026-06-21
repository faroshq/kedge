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
	"fmt"
	"log"
	"os"

	sdkinstall "github.com/faroshq/provider-sdk/install"

	"github.com/faroshq/provider-sandbox/install"
)

func runInitCmd(ctx context.Context) error {
	config, err := loadProviderConfig()
	if err != nil {
		return fmt.Errorf("init needs a kubeconfig (set KEDGE_PROVIDER_KUBECONFIG): %w", err)
	}
	workspacePath := os.Getenv("SANDBOX_WORKSPACE_PATH")
	if workspacePath == "" {
		workspacePath = install.DefaultWorkspacePath
	}
	schemasDir := os.Getenv("KEDGE_SCHEMAS_DIR")
	if schemasDir == "" {
		schemasDir = "/etc/kedge/schemas"
	}
	if err := sdkinstall.Bootstrap(ctx, sdkinstall.Options{
		Config:           config,
		ExportName:       install.APIExportName,
		WorkspacePath:    workspacePath,
		SchemasDir:       schemasDir,
		CatalogEntryFile: os.Getenv("KEDGE_CATALOGENTRY_FILE"),
	}); err != nil {
		return fmt.Errorf("provider workspace bootstrap: %w", err)
	}
	log.Printf("sandbox init: workspace bootstrapped (export=%s path=%s schemas=%s)", install.APIExportName, workspacePath, schemasDir)
	return nil
}
