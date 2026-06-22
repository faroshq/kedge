/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package devenvironment

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	sandboxv1alpha1 "github.com/faroshq/provider-sandbox/apis/v1alpha1"
)

func TestEnsureRuntimeObjectsCreatesPVCDeploymentServiceAndPreviewStatus(t *testing.T) {
	ctx := context.Background()
	runtimeClient := fake.NewSimpleClientset()
	r := &RuntimeReconciler{
		RuntimeClient: runtimeClient,
		ClusterName:   "root:kedge:tenants:org:workspace",
	}
	env := &sandboxv1alpha1.DevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "todo-dev", Generation: 7},
		Spec: sandboxv1alpha1.DevEnvironmentSpec{
			ProjectRef: "todo",
			Runtime: sandboxv1alpha1.DevEnvironmentRuntime{
				Image:        "ghcr.io/faroshq/kedge-sandbox-runner:dev",
				WorkingDir:   "/workspace",
				StartCommand: "npm run dev -- --host 0.0.0.0",
				Port:         3000,
			},
			Sync: sandboxv1alpha1.DevEnvironmentSync{Mode: "patch"},
		},
	}

	status, err := r.EnsureRuntimeObjects(ctx, env)
	if err != nil {
		t.Fatalf("EnsureRuntimeObjects: %v", err)
	}

	ns := runtimeNamespace("root:kedge:tenants:org:workspace")
	if strings.Contains(ns, ":") {
		t.Fatalf("runtime namespace %q contains ':'", ns)
	}
	namespace, err := runtimeClient.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace %s not created: %v", ns, err)
	}
	if got := namespace.Annotations[AnnotationLogicalCluster]; got != "root:kedge:tenants:org:workspace" {
		t.Fatalf("namespace logical cluster annotation = %q", got)
	}
	if _, err := runtimeClient.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName(env.Name), metav1.GetOptions{}); err != nil {
		t.Fatalf("PVC not created: %v", err)
	}
	deploy, err := runtimeClient.AppsV1().Deployments(ns).Get(ctx, deploymentName(env.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Deployment not created: %v", err)
	}
	assertRunnerDeployment(t, deploy, env)
	svc, err := runtimeClient.CoreV1().Services(ns).Get(ctx, serviceName(env.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service not created: %v", err)
	}
	assertPreviewService(t, svc, env.Name)

	if got, want := status.PreviewURL, "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"; got != want {
		t.Fatalf("PreviewURL = %q, want %q", got, want)
	}
	if got, want := status.Phase, sandboxv1alpha1.DevEnvironmentPhaseProvisioning; got != want {
		t.Fatalf("Phase = %q, want %q", got, want)
	}
	if got, want := status.ObservedGeneration, int64(7); got != want {
		t.Fatalf("ObservedGeneration = %d, want %d", got, want)
	}
}

func assertRunnerDeployment(t *testing.T, deploy *appsv1.Deployment, env *sandboxv1alpha1.DevEnvironment) {
	t.Helper()
	labels := deploy.Spec.Template.Labels
	if got := labels[LabelDevEnvironment]; got != env.Name {
		t.Fatalf("pod label %s = %q, want %q", LabelDevEnvironment, got, env.Name)
	}
	if deploy.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != pvcName(env.Name) {
		t.Fatalf("deployment PVC claim = %q", deploy.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	}
	container := deploy.Spec.Template.Spec.Containers[0]
	if got := container.Image; got != env.Spec.Runtime.Image {
		t.Fatalf("container image = %q, want %q", got, env.Spec.Runtime.Image)
	}
	envs := map[string]string{}
	for _, e := range container.Env {
		envs[e.Name] = e.Value
	}
	for name, want := range map[string]string{
		"SANDBOX_WORKDIR":       env.Spec.Runtime.WorkingDir,
		"SANDBOX_START_COMMAND": env.Spec.Runtime.StartCommand,
		"SANDBOX_PORT":          "3000",
	} {
		if got := envs[name]; got != want {
			t.Fatalf("env %s = %q, want %q", name, got, want)
		}
	}
	if got := container.Ports[0].Name; got != "preview" {
		t.Fatalf("first port = %q, want preview", got)
	}
	if got := container.Ports[1].Name; got != "control" {
		t.Fatalf("second port = %q, want control", got)
	}
}

func assertPreviewService(t *testing.T, svc *corev1.Service, envName string) {
	t.Helper()
	if got := svc.Spec.Selector[LabelDevEnvironment]; got != envName {
		t.Fatalf("service selector = %q, want %q", got, envName)
	}
	ports := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	for name, want := range map[string]int32{"preview": 3000, "control": 7070} {
		if got := ports[name]; got != want {
			t.Fatalf("service port %s = %d, want %d", name, got, want)
		}
	}
}

func TestDeploymentAvailabilityDrivesRunningStatus(t *testing.T) {
	ctx := context.Background()
	runtimeClient := fake.NewSimpleClientset()
	r := &RuntimeReconciler{RuntimeClient: runtimeClient, ClusterName: "root:kedge:tenants:org:workspace"}
	env := &sandboxv1alpha1.DevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "todo-dev", Generation: 1},
		Spec: sandboxv1alpha1.DevEnvironmentSpec{
			Runtime: sandboxv1alpha1.DevEnvironmentRuntime{Image: "runner", WorkingDir: "/workspace", StartCommand: "npm run dev", Port: 3000},
		},
	}
	if _, err := r.EnsureRuntimeObjects(ctx, env); err != nil {
		t.Fatalf("initial EnsureRuntimeObjects: %v", err)
	}
	deploy, err := runtimeClient.AppsV1().Deployments(runtimeNamespace(r.ClusterName)).Get(ctx, deploymentName(env.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	deploy.Status.AvailableReplicas = 1
	if _, err := runtimeClient.AppsV1().Deployments(runtimeNamespace(r.ClusterName)).UpdateStatus(ctx, deploy, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update deployment status: %v", err)
	}

	status, err := r.EnsureRuntimeObjects(ctx, env)
	if err != nil {
		t.Fatalf("second EnsureRuntimeObjects: %v", err)
	}
	if got, want := status.Phase, sandboxv1alpha1.DevEnvironmentPhaseRunning; got != want {
		t.Fatalf("Phase = %q, want %q", got, want)
	}
	if cond := findCondition(status.Conditions, sandboxv1alpha1.ConditionReady); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("Ready condition = %#v, want true", cond)
	}
}

func TestRuntimeStartCommandDefaultSupportsNodeStartFallback(t *testing.T) {
	env := &sandboxv1alpha1.DevEnvironment{}
	got := runtimeStartCommand(env)
	for _, want := range []string{
		"npm install --no-audit --no-fund",
		"npm run dev -- --host 0.0.0.0 --port \"$PORT\"",
		"npm start",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("runtimeStartCommand() = %q, want substring %q", got, want)
		}
	}
}

func TestRuntimeObjectNamesAreDeterministicAndKubernetesSafe(t *testing.T) {
	ns := runtimeNamespace("root:kedge:tenants:org:workspace")
	if !strings.HasPrefix(ns, "sandbox-") || strings.Contains(ns, ":") || len(ns) > 63 {
		t.Fatalf("runtimeNamespace = %q, want sandbox-* DNS label without ':'", ns)
	}
	if got, want := pvcName("todo-dev"), "todo-dev-workspace"; got != want {
		t.Fatalf("pvcName = %q, want %q", got, want)
	}
	if got, want := deploymentName("todo-dev"), "todo-dev-runner"; got != want {
		t.Fatalf("deploymentName = %q, want %q", got, want)
	}
	if got, want := serviceName("todo-dev"), "todo-dev-preview"; got != want {
		t.Fatalf("serviceName = %q, want %q", got, want)
	}
}
