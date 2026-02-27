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

// Package site reconciles Site resources.
package site

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	kedgev1alpha1 "github.com/faroshq/faros-kedge/apis/kedge/v1alpha1"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// HeartbeatTimeout is the duration after which a site is considered disconnected.
	// Matches the edge controller: 90s = 3 missed heartbeats at the agent 30s interval.
	HeartbeatTimeout = 90 * time.Second
)

// HeartbeatReconciler monitors Site connectivity and marks stale sites as Disconnected.
type HeartbeatReconciler struct {
	mgr mcmanager.Manager
}

// SetupHeartbeatWithManager registers the site heartbeat controller with the multicluster manager.
func SetupHeartbeatWithManager(mgr mcmanager.Manager) error {
	r := &HeartbeatReconciler{mgr: mgr}
	return mcbuilder.ControllerManagedBy(mgr).
		Named("site-heartbeat").
		For(&kedgev1alpha1.Site{}).
		Complete(r)
}

// Reconcile checks a Site's last heartbeat time and transitions its phase to Disconnected
// when the heartbeat has not been updated within HeartbeatTimeout.
func (r *HeartbeatReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := klog.FromContext(ctx).WithValues("site", req.Name, "cluster", req.ClusterName)

	cl, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting cluster %s: %w", req.ClusterName, err)
	}
	c := cl.GetClient()

	var site kedgev1alpha1.Site
	if err := c.Get(ctx, req.NamespacedName, &site); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Nothing to do if the tunnel is already marked as disconnected.
	if !site.Status.TunnelConnected {
		return ctrl.Result{RequeueAfter: HeartbeatTimeout}, nil
	}

	// Mark as Disconnected when the heartbeat is stale.
	if site.Status.LastHeartbeatTime != nil && time.Since(site.Status.LastHeartbeatTime.Time) > HeartbeatTimeout {
		logger.Info("Site heartbeat timed out, marking Disconnected",
			"lastHeartbeatTime", site.Status.LastHeartbeatTime.Time,
			"elapsed", time.Since(site.Status.LastHeartbeatTime.Time).Round(time.Second),
		)
		site.Status.Phase = kedgev1alpha1.SitePhaseDisconnected
		site.Status.TunnelConnected = false
		if err := c.Status().Update(ctx, &site); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating site status: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Re-check again after the timeout window has elapsed.
	return ctrl.Result{RequeueAfter: HeartbeatTimeout}, nil
}
