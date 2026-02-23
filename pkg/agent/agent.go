/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package agent implements the kedge agent that connects sites to the hub.
package agent

import (
	"context"
	"crypto/tls"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	kedgev1alpha1 "github.com/faroshq/faros-kedge/apis/kedge/v1alpha1"
	"github.com/faroshq/faros-kedge/pkg/agent/reconciler"
	agentStatus "github.com/faroshq/faros-kedge/pkg/agent/status"
	"github.com/faroshq/faros-kedge/pkg/agent/tunnel"
	kedgeclient "github.com/faroshq/faros-kedge/pkg/client"
)

const (
	// AgentModeSite is the default mode: connects a Kubernetes cluster to the hub.
	AgentModeSite = "site"
	// AgentModeServer is the systemd/bare-metal mode: connects a non-k8s host
	// to the hub, exposing SSH access via the reverse tunnel.
	AgentModeServer = "server"
)

// Options holds configuration for the agent.
type Options struct {
	HubURL        string
	HubKubeconfig string
	HubContext    string
	TunnelURL     string // Separate URL for reverse tunnel (defaults to hubConfig.Host)
	Token         string
	SiteName      string
	Kubeconfig    string
	Context       string
	Labels        map[string]string
	// Mode controls whether the agent registers as a Site (k8s cluster) or a
	// Server (bare-metal / systemd host). Defaults to AgentModeSite.
	Mode string
	// InsecureSkipTLSVerify disables TLS certificate verification for the hub
	// connection. Should only be used in development/testing; never in production.
	InsecureSkipTLSVerify bool
}

// NewOptions returns default agent options.
func NewOptions() *Options {
	return &Options{
		Labels: make(map[string]string),
		Mode:   AgentModeSite,
	}
}

// Agent is the kedge agent that connects a site or server to the hub.
type Agent struct {
	opts             *Options
	hubConfig        *rest.Config
	hubTLSConfig     *tls.Config
	downstreamConfig *rest.Config // nil in server mode
}

// New creates a new agent.
func New(opts *Options) (*Agent, error) {
	if opts.SiteName == "" {
		return nil, fmt.Errorf("site name is required")
	}
	if opts.Mode != AgentModeSite && opts.Mode != AgentModeServer {
		return nil, fmt.Errorf("invalid mode %q: must be %q or %q", opts.Mode, AgentModeSite, AgentModeServer)
	}

	// Build hub config
	var hubConfig *rest.Config
	var err error
	if opts.HubKubeconfig != "" {
		rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: opts.HubKubeconfig}
		overrides := &clientcmd.ConfigOverrides{}
		if opts.HubContext != "" {
			overrides.CurrentContext = opts.HubContext
		}
		hubConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build hub config from kubeconfig: %w", err)
		}
		if opts.HubURL != "" {
			hubConfig.Host = opts.HubURL
		}
	} else if opts.HubURL != "" {
		hubConfig = &rest.Config{
			Host:        opts.HubURL,
			BearerToken: opts.Token,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: opts.InsecureSkipTLSVerify,
			},
		}
	} else {
		return nil, fmt.Errorf("hub URL or hub kubeconfig is required")
	}

	hubTLSConfig, err := rest.TLSConfigFor(hubConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build hub TLS config: %w", err)
	}

	a := &Agent{
		opts:         opts,
		hubConfig:    hubConfig,
		hubTLSConfig: hubTLSConfig,
	}

	// In server mode there is no downstream Kubernetes cluster to connect to.
	if opts.Mode == AgentModeSite {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		if opts.Kubeconfig != "" {
			rules.ExplicitPath = opts.Kubeconfig
		}
		overrides := &clientcmd.ConfigOverrides{}
		if opts.Context != "" {
			overrides.CurrentContext = opts.Context
		}
		a.downstreamConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build downstream config: %w", err)
		}
	}

	return a, nil
}

// Run starts the agent and blocks until the context is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	logger.Info("Starting kedge agent",
		"siteName", a.opts.SiteName,
		"mode", a.opts.Mode,
		"labels", a.opts.Labels,
	)

	hubDynamic, err := dynamic.NewForConfig(a.hubConfig)
	if err != nil {
		return fmt.Errorf("creating hub dynamic client: %w", err)
	}
	hubClient := kedgeclient.NewFromDynamic(hubDynamic)

	if a.opts.Mode == AgentModeServer {
		return a.runServerMode(ctx, logger, hubClient)
	}
	return a.runSiteMode(ctx, logger, hubClient)
}

// runSiteMode is the original Kubernetes-cluster mode.
func (a *Agent) runSiteMode(ctx context.Context, logger klog.Logger, hubClient *kedgeclient.Client) error {
	downstreamClient, err := kubernetes.NewForConfig(a.downstreamConfig)
	if err != nil {
		return fmt.Errorf("creating downstream client: %w", err)
	}

	if err := a.registerSite(ctx, hubClient); err != nil {
		return fmt.Errorf("registering site: %w", err)
	}
	logger.Info("Site registered")

	tunnelURL := a.opts.TunnelURL
	if tunnelURL == "" {
		tunnelURL = a.hubConfig.Host
	}
	tunnelState := make(chan bool, 1)
	go tunnel.StartProxyTunnel(ctx, tunnelURL, a.hubConfig.BearerToken, a.opts.SiteName, a.downstreamConfig, a.hubTLSConfig, tunnelState)

	wkr := reconciler.NewWorkloadReconciler(a.opts.SiteName, hubClient, hubClient.Dynamic(), downstreamClient)
	go func() {
		if err := wkr.Run(ctx); err != nil {
			logger.Error(err, "Workload reconciler failed")
		}
	}()

	reporter := agentStatus.NewReporter(a.opts.SiteName, hubClient, downstreamClient)
	go func() {
		if err := reporter.Run(ctx); err != nil {
			logger.Error(err, "Status reporter failed")
		}
	}()

	logger.Info("Agent started successfully (site mode)")
	<-ctx.Done()
	logger.Info("Agent shutting down")
	return nil
}

// runServerMode is the bare-metal / systemd mode: no k8s, just SSH over revdial.
func (a *Agent) runServerMode(ctx context.Context, logger klog.Logger, hubClient *kedgeclient.Client) error {
	if err := a.registerServer(ctx, hubClient); err != nil {
		return fmt.Errorf("registering server: %w", err)
	}
	logger.Info("Server registered")

	tunnelURL := a.opts.TunnelURL
	if tunnelURL == "" {
		tunnelURL = a.hubConfig.Host
	}
	tunnelState := make(chan bool, 1)
	// downstreamConfig is nil in server mode; the tunnel only serves /ssh.
	go tunnel.StartProxyTunnel(ctx, tunnelURL, a.hubConfig.BearerToken, a.opts.SiteName, nil, a.hubTLSConfig, tunnelState)

	reporter := agentStatus.NewServerReporter(a.opts.SiteName, hubClient)
	go func() {
		if err := reporter.Run(ctx); err != nil {
			logger.Error(err, "Server status reporter failed")
		}
	}()

	logger.Info("Agent started successfully (server mode)")
	<-ctx.Done()
	logger.Info("Agent shutting down")
	return nil
}

func (a *Agent) registerSite(ctx context.Context, client *kedgeclient.Client) error {
	logger := klog.FromContext(ctx)

	site := &kedgev1alpha1.Site{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kedgev1alpha1.SchemeGroupVersion.String(),
			Kind:       "Site",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   a.opts.SiteName,
			Labels: a.opts.Labels,
		},
		Spec: kedgev1alpha1.SiteSpec{
			DisplayName: a.opts.SiteName,
		},
	}

	existing, err := client.Sites().Get(ctx, a.opts.SiteName, metav1.GetOptions{})
	if err != nil {
		logger.Info("Creating Site", "name", a.opts.SiteName)
		_, err := client.Sites().Create(ctx, site, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating site: %w", err)
		}
	} else {
		logger.Info("Updating Site", "name", a.opts.SiteName)
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		for k, v := range a.opts.Labels {
			existing.Labels[k] = v
		}
		_, err := client.Sites().Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating site: %w", err)
		}
	}

	return nil
}

func (a *Agent) registerServer(ctx context.Context, client *kedgeclient.Client) error {
	logger := klog.FromContext(ctx)

	server := &kedgev1alpha1.Server{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kedgev1alpha1.SchemeGroupVersion.String(),
			Kind:       "Server",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   a.opts.SiteName,
			Labels: a.opts.Labels,
		},
		Spec: kedgev1alpha1.ServerSpec{
			DisplayName: a.opts.SiteName,
		},
	}

	existing, err := client.Servers().Get(ctx, a.opts.SiteName, metav1.GetOptions{})
	if err != nil {
		logger.Info("Creating Server", "name", a.opts.SiteName)
		_, err := client.Servers().Create(ctx, server, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating server: %w", err)
		}
	} else {
		logger.Info("Updating Server", "name", a.opts.SiteName)
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		for k, v := range a.opts.Labels {
			existing.Labels[k] = v
		}
		_, err := client.Servers().Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating server: %w", err)
		}
	}

	return nil
}
