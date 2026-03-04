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

package edge

import (
	"context"
	"fmt"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	kedgev1alpha1 "github.com/faroshq/faros-kedge/apis/kedge/v1alpha1"
	"github.com/faroshq/faros-kedge/pkg/hub/kcp"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

var workspaceGVR = schema.GroupVersionResource{
	Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces",
}

// MountReconciler watches Edges and creates mount Workspaces in kcp for
// type=kubernetes edges so that users can access edge kube APIs through kcp
// workspace navigation.
//
// Edges with spec.type=server are skipped — they have no kcp workspace.
type MountReconciler struct {
	mgr            mcmanager.Manager
	kcpConfig      *rest.Config
	hubExternalURL string

	// workspaceEnsureFn creates or adopts the kcp mount workspace for an edge and
	// returns the URL that kcp has assigned to the workspace (Workspace.Spec.URL).
	// Returns ("", nil) when the workspace exists but kcp has not yet assigned a URL.
	// Defaults to r.ensureMountWorkspace; injectable for unit testing.
	workspaceEnsureFn func(ctx context.Context, logger klog.Logger, clusterName string, edge *kedgev1alpha1.Edge) (string, error)
}

// SetupMountWithManager registers the edge mount controller with the multicluster manager.
func SetupMountWithManager(mgr mcmanager.Manager, kcpConfig *rest.Config, hubExternalURL string) error {
	r := &MountReconciler{
		mgr:            mgr,
		kcpConfig:      kcpConfig,
		hubExternalURL: hubExternalURL,
	}
	r.workspaceEnsureFn = r.ensureMountWorkspace
	return mcbuilder.ControllerManagedBy(mgr).
		Named("edge-mount").
		For(&kedgev1alpha1.Edge{}).
		Owns(&kcptenancyv1alpha1.Workspace{}).
		Complete(r)
}

// Reconcile creates a mount workspace for a ready kubernetes edge and maintains
// the URL status field using the URL assigned by kcp (Workspace.Spec.URL).
func (r *MountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := klog.FromContext(ctx).WithValues("edge", req.Name, "cluster", req.ClusterName)

	cl, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting cluster %s: %w", req.ClusterName, err)
	}
	c := cl.GetClient()

	var edge kedgev1alpha1.Edge
	if err := c.Get(ctx, req.NamespacedName, &edge); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Only proceed if the edge is ready (agent connected).
	if edge.Status.Phase != kedgev1alpha1.EdgePhaseReady {
		return ctrl.Result{}, nil
	}

	// Guard: server-type edges do NOT get a kcp workspace.
	// If the type changed from kubernetes to server, delete the existing workspace.
	if edge.Spec.Type == kedgev1alpha1.EdgeTypeServer {
		logger.V(4).Info("Server-type edge, ensuring no workspace exists")
		if err := r.deleteMountWorkspace(ctx, logger, req.ClusterName, edge.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting mount workspace for server edge: %w", err)
		}
		// Set the SSH URL on the edge status so clients can reach the SSH endpoint.
		expectedSSHURL := r.hubExternalURL + "/services/edges-proxy/clusters/" + req.ClusterName +
			"/apis/kedge.faros.sh/v1alpha1/edges/" + edge.Name + "/ssh"
		if edge.Status.URL != expectedSSHURL {
			logger.Info("Setting edge SSH URL", "url", expectedSSHURL)
			edge.Status.URL = expectedSSHURL
			if err := c.Status().Update(ctx, &edge); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating edge SSH URL: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// At this point edge.Spec.Type == EdgeTypeKubernetes.

	// Create or adopt the mount workspace in kcp via admin dynamic client.
	// The URL is read from the kcp Workspace.Spec.URL field once kcp has assigned it.
	workspaceURL, err := r.workspaceEnsureFn(ctx, logger, req.ClusterName, &edge)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring mount workspace: %w", err)
	}

	// kcp may not have assigned a URL yet (workspace still initialising).
	// The Owns(Workspace) watch will trigger a re-reconcile once kcp updates the workspace.
	if workspaceURL == "" {
		logger.V(4).Info("Mount workspace URL not yet assigned by kcp, waiting")
		return ctrl.Result{}, nil
	}

	// Set the workspace URL on the edge status if it differs from what kcp assigned.
	if edge.Status.URL != workspaceURL {
		logger.Info("Setting edge workspace URL from kcp Workspace spec", "url", workspaceURL)
		edge.Status.URL = workspaceURL
		if err := c.Status().Update(ctx, &edge); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating edge workspaceURL: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// ensureMountWorkspace creates a Workspace with mount.ref pointing to the Edge,
// owned by the Edge so that the workspace is garbage-collected when the Edge is deleted.
// It returns the URL from Workspace.Spec.URL that kcp assigns once the mount is initialised.
// Returns ("", nil) when the workspace exists but kcp has not yet populated spec.URL.
func (r *MountReconciler) ensureMountWorkspace(ctx context.Context, logger klog.Logger, clusterName string, edge *kedgev1alpha1.Edge) (string, error) {
	cfg := rest.CopyConfig(r.kcpConfig)
	cfg.Host = kcp.AppendClusterPath(cfg.Host, clusterName)

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("creating dynamic client: %w", err)
	}

	ws := &kcptenancyv1alpha1.Workspace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kcptenancyv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Workspace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            edge.Name,
			OwnerReferences: []metav1.OwnerReference{edgeOwnerRef(edge)},
		},
		Spec: kcptenancyv1alpha1.WorkspaceSpec{
			Mount: &kcptenancyv1alpha1.Mount{
				Reference: kcptenancyv1alpha1.ObjectReference{
					APIVersion: kedgev1alpha1.SchemeGroupVersion.String(),
					Kind:       "Edge",
					Name:       edge.Name,
				},
			},
		},
	}

	u, err := toUnstructured(ws)
	if err != nil {
		return "", fmt.Errorf("converting workspace to unstructured: %w", err)
	}

	_, createErr := client.Resource(workspaceGVR).Create(ctx, u, metav1.CreateOptions{})
	if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return "", fmt.Errorf("creating mount workspace %s: %w", edge.Name, createErr)
	}
	if createErr == nil {
		logger.Info("Mount workspace created", "edge", edge.Name, "cluster", clusterName)
	} else {
		logger.V(4).Info("Mount workspace already exists", "edge", edge.Name)
	}

	// Read the workspace back to obtain the URL that kcp has assigned in spec.URL.
	// kcp sets this field once the mount workspace has been fully initialised.
	existing, err := client.Resource(workspaceGVR).Get(ctx, edge.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting mount workspace %s: %w", edge.Name, err)
	}

	// spec.URL is the canonical URL for this workspace as assigned by kcp.
	workspaceURL, _, _ := unstructured.NestedString(existing.Object, "spec", "URL")
	return workspaceURL, nil
}

// deleteMountWorkspace deletes the mount workspace for an edge if it exists.
// This is called when an edge type changes from kubernetes to server.
func (r *MountReconciler) deleteMountWorkspace(ctx context.Context, logger klog.Logger, clusterName, edgeName string) error {
	cfg := rest.CopyConfig(r.kcpConfig)
	cfg.Host = kcp.AppendClusterPath(cfg.Host, clusterName)

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	err = client.Resource(workspaceGVR).Delete(ctx, edgeName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		logger.V(4).Info("Mount workspace already deleted or never existed", "edge", edgeName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting mount workspace %s: %w", edgeName, err)
	}

	logger.Info("Mount workspace deleted", "edge", edgeName, "cluster", clusterName)
	return nil
}

// toUnstructured converts a typed runtime.Object to an Unstructured object.
func toUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: data}, nil
}
