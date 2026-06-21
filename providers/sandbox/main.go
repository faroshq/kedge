/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// sandbox is a dedicated live development runtime provider. It owns
// DevEnvironment resources, runtime pods/PVCs/services, file sync, logs, and
// preview proxying so App Studio stays capability-oriented.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/faroshq/provider-sandbox/server"
	"github.com/faroshq/provider-sandbox/tenant"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err := runInitCmd(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "init:", err)
				os.Exit(1)
			}
			return
		case "serve":
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\nusage: sandbox-provider [init|serve]\n", os.Args[1])
			os.Exit(2)
		}
	}
	runServe()
}

func runServe() {
	port := envOr("PORT", "8086")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	providerConfig, providerErr := loadProviderConfig()
	if providerErr != nil {
		log.Printf("kcp config unavailable (%v); tenant backend + controller disabled", providerErr)
	}
	runtimeConfig, runtimeErr := loadRuntimeConfig()
	if runtimeErr != nil {
		log.Printf("runtime config unavailable (%v); runtime controller and proxies disabled", runtimeErr)
	}
	tenantFactory := tenant.NewClientFactory(providerConfig)

	handler, err := newHandler(server.New(runtimeConfig, tenantFactory))
	if err != nil {
		log.Fatalf("portal embed: %v", err)
	}
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           logMiddleware(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("sandbox provider listening on :%s", port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	if err := startControllerManager(ctx, providerConfig, runtimeConfig); err != nil {
		if errors.Is(err, errControllerDisabled) {
			log.Printf("controller manager disabled: %v", err)
		} else {
			log.Printf("controller manager not started: %v", err)
		}
	}
	go runHeartbeat(ctx)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdown); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func newHandler(apiHandler http.Handler) (http.Handler, error) {
	r := mux.NewRouter()
	if apiHandler != nil {
		r.PathPrefix("/api/").Handler(apiHandler)
		r.Handle("/healthz", apiHandler)
	}
	fileServer, distFS, err := portalHandler()
	if err != nil {
		return nil, err
	}
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := strings.TrimPrefix(req.URL.Path, "/")
		if clean != "" && servePortalAsset(w, req, distFS, clean) {
			return
		}
		req2 := req.Clone(req.Context())
		req2.URL.Path = "/"
		fileServer.ServeHTTP(w, req2)
	})
	return r, nil
}

func loadProviderConfig() (*rest.Config, error) {
	for _, path := range []string{
		os.Getenv("KEDGE_PROVIDER_KUBECONFIG"),
		os.Getenv("SANDBOX_KUBECONFIG"),
		"/var/run/secrets/kedge/kedge-provider-kubeconfig",
		os.Getenv("KUBECONFIG"),
	} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		cfg, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("loading kubeconfig %s: %w", path, err)
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, errControllerDisabled
	}
	return cfg, nil
}

func loadRuntimeConfig() (*rest.Config, error) {
	for _, path := range []string{os.Getenv("SANDBOX_RUNTIME_KUBECONFIG"), os.Getenv("KUBECONFIG")} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		cfg, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("loading runtime kubeconfig %s: %w", path, err)
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, errControllerDisabled
	}
	return cfg, nil
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
