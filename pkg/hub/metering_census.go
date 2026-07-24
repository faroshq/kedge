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

package hub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	meteringv1alpha1 "github.com/kcp-dev/contrib-metering/sdk/apis/metering/v1alpha1"

	"github.com/faroshq/faros-kedge/pkg/apiurl"
	"github.com/faroshq/faros-kedge/pkg/kcppaths"
)

// membershipReportGVR addresses the platform-only MembershipReport type, served
// in the metering platform workspace via the metering-platform APIBinding.
var membershipReportGVR = schema.GroupVersionResource{
	Group: meteringv1alpha1.GroupName, Version: "v1alpha1", Resource: "membershipreports",
}

// meteringCensus is kedge's platform-specific membership emitter: it discovers the
// real workspace topology and pushes it into contrib-metering as MembershipReports,
// the authoritative statement of which workspaces belong to which billing account.
// contrib-metering stays topology-agnostic — this is the one component that knows
// kedge's tree (orgs under root:kedge:tenants, each org a billing boundary whose
// descendants pool into its account).
//
// It is a census, not an event stream: each pass reports the ABSOLUTE current
// membership per org and upserts one MembershipReport per account (deterministic
// name), so it is idempotent and self-healing — a missed create/delete is corrected
// on the next pass. The metering controller folds these into its membership index
// (cluster->account for usage attribution + enforcement, path->account for
// Entitlement projection) and derives the drift-free `workspaces` count from the
// member cardinality.
type meteringCensus struct {
	// kcpConfig is the hub's kcp admin config (front-proxy base). The census scopes
	// its host per request: "*" for the wildcard LogicalCluster list, and the
	// platform workspace path to write reports.
	kcpConfig *rest.Config
	// reporter identifies this emitter in the report name/spec, so multiple
	// reporters union rather than clobber in the controller.
	reporter string
	// interval is how often the census runs.
	interval time.Duration
	log      logr.Logger
}

// Run polls the census until ctx is cancelled.
func (c *meteringCensus) Run(ctx context.Context) {
	if c.interval == 0 {
		c.interval = time.Minute
	}
	if c.reporter == "" {
		c.reporter = "kedge-census"
	}
	// Run once promptly, then on the interval.
	if err := c.reconcile(ctx); err != nil {
		c.log.Error(err, "metering census: initial pass failed")
	}
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.reconcile(ctx); err != nil {
				c.log.Error(err, "metering census: pass failed")
			}
		}
	}
}

// tenantWorkspace is one discovered workspace under root:kedge:tenants.
type tenantWorkspace struct {
	cluster string // LogicalCluster id (kcp.io/cluster)
	path    string // canonical path (kcp.io/path)
}

// reconcile discovers the current tenant-workspace topology and upserts one
// MembershipReport per org account.
func (c *meteringCensus) reconcile(ctx context.Context) error {
	workspaces, err := c.listTenantWorkspaces(ctx)
	if err != nil {
		return err
	}

	// Group by org account. Each org's LogicalCluster id IS the account name (the
	// initializer names the Account after the boundary workspace's cluster). Members
	// pool the org itself plus every descendant under its path.
	reports := membersByAccount(workspaces)
	if len(reports) == 0 {
		c.log.V(3).Info("metering census: no tenant orgs discovered")
		return nil
	}

	writer, err := dynamic.NewForConfig(c.clusterConfig(kcppaths.SystemMeteringPlatform))
	if err != nil {
		return fmt.Errorf("metering census: platform client: %w", err)
	}
	var firstErr error
	for account, members := range reports {
		if err := c.upsert(ctx, writer, account, members); err != nil {
			c.log.Error(err, "metering census: upsert report", "account", account)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	c.log.V(2).Info("metering census pass complete", "accounts", len(reports), "workspaces", len(workspaces))
	return firstErr
}

// listTenantWorkspaces wildcard-lists LogicalClusters across the shard and keeps
// those whose path is at or under root:kedge:tenants.
func (c *meteringCensus) listTenantWorkspaces(ctx context.Context) ([]tenantWorkspace, error) {
	cl, err := dynamic.NewForConfig(c.clusterConfig("*"))
	if err != nil {
		return nil, fmt.Errorf("metering census: wildcard client: %w", err)
	}
	list, err := cl.Resource(logicalClusterGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("metering census: list LogicalClusters: %w", err)
	}
	prefix := workspacePathRoot + ":"
	out := make([]tenantWorkspace, 0, len(list.Items))
	for i := range list.Items {
		ann := list.Items[i].GetAnnotations()
		path := ann["kcp.io/path"]
		id := ann["kcp.io/cluster"]
		if path == "" || id == "" {
			continue
		}
		if path != workspacePathRoot && !strings.HasPrefix(path, prefix) {
			continue
		}
		out = append(out, tenantWorkspace{cluster: id, path: path})
	}
	return out, nil
}

// clusterConfig returns a copy of the kcp config with the host scoped to a cluster
// path (or "*" for the wildcard list endpoint).
func (c *meteringCensus) clusterConfig(cluster string) *rest.Config {
	cfg := rest.CopyConfig(c.kcpConfig)
	cfg.Host = apiurl.KCPClusterURL(cfg.Host, cluster)
	return cfg
}

// upsert creates or updates the MembershipReport for one account. The name is
// deterministic per (reporter, account) so passes idempotently replace it.
func (c *meteringCensus) upsert(ctx context.Context, writer dynamic.Interface, account string, members []meteringv1alpha1.MemberWorkspace) error {
	name := c.reporter + "-" + account
	desired := &meteringv1alpha1.MembershipReport{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: meteringv1alpha1.MembershipReportSpec{
			Account:    account,
			Reporter:   c.reporter,
			Members:    members,
			ObservedAt: metav1.Now(),
		},
	}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	obj := &unstructured.Unstructured{Object: u}
	obj.SetAPIVersion(meteringv1alpha1.GroupName + "/v1alpha1")
	obj.SetKind("MembershipReport")

	existing, err := writer.Resource(membershipReportGVR).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = writer.Resource(membershipReportGVR).Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	_, err = writer.Resource(membershipReportGVR).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

// membersByAccount groups discovered workspaces into per-account member sets. An
// account is keyed by the org's LogicalCluster id (the org workspace is the one
// whose path is exactly root:kedge:tenants:<org>); its members are the org plus
// every workspace whose path is under the org path. Descendants of an org whose own
// LogicalCluster was not listed are still attributed by path prefix.
func membersByAccount(workspaces []tenantWorkspace) map[string][]meteringv1alpha1.MemberWorkspace {
	// orgPath -> account id (the org's own cluster).
	orgAccount := map[string]string{}
	for _, w := range workspaces {
		if orgPath, ok := orgPathOf(w.path); ok && orgPath == w.path {
			orgAccount[orgPath] = w.cluster
		}
	}
	out := map[string][]meteringv1alpha1.MemberWorkspace{}
	for _, w := range workspaces {
		orgPath, ok := orgPathOf(w.path)
		if !ok {
			continue
		}
		account, ok := orgAccount[orgPath]
		if !ok {
			// Org boundary LogicalCluster not seen this pass; skip until it is (the
			// account id is unknown without it). Self-heals next pass.
			continue
		}
		out[account] = append(out[account], meteringv1alpha1.MemberWorkspace{Cluster: w.cluster, Path: w.path})
	}
	return out
}

// orgPathOf returns the org-boundary path (root:kedge:tenants:<org>) for a tenant
// workspace path, and whether the path is a tenant workspace at all. The org is the
// first four colon-separated segments.
func orgPathOf(path string) (string, bool) {
	prefix := workspacePathRoot + ":"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return "", false
	}
	org := rest
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		org = rest[:i]
	}
	return prefix + org, true
}
