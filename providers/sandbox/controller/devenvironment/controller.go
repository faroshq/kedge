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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/faroshq/provider-sandbox/apis/v1alpha1"
)

const (
	LabelDevEnvironment      = "sandbox.kedge.faros.sh/dev-environment"
	LabelTenantHash          = "sandbox.kedge.faros.sh/tenant-hash"
	AnnotationLogicalCluster = "sandbox.kedge.faros.sh/logical-cluster"

	DefaultRunnerImage  = "ghcr.io/faroshq/kedge-sandbox-runner:dev"
	DefaultStartCommand = `export PORT="${SANDBOX_PORT:-3000}"; ` +
		`if [ -f package.json ]; then ` +
		`npm install --no-audit --no-fund && (npm run dev -- --host 0.0.0.0 --port "$PORT" || npm start); ` +
		`else npx --yes vite --host 0.0.0.0 --port "$PORT"; fi`
	DefaultRuntimePort    = int32(3000)
	DefaultRuntimeWorkDir = "/workspace"
)

type Reconciler struct {
	Manager       mcmanager.Manager
	RuntimeClient kubernetes.Interface
}

func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	r.Manager = mgr
	return mcbuilder.ControllerManagedBy(mgr).
		Named("sandbox-devenvironment").
		For(&sandboxv1alpha1.DevEnvironment{}).
		Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := klog.FromContext(ctx).WithValues("devenvironment", req.Name, "cluster", req.ClusterName)
	c, err := clusterClient(ctx, r.Manager, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}
	var env sandboxv1alpha1.DevEnvironment
	if err := c.Get(ctx, req.NamespacedName, &env); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	status, err := (&RuntimeReconciler{
		RuntimeClient: r.RuntimeClient,
		ClusterName:   req.ClusterName.String(),
	}).EnsureRuntimeObjects(ctx, &env)
	if err != nil {
		env.Status.Phase = sandboxv1alpha1.DevEnvironmentPhaseFailed
		setCondition(&env.Status.Conditions, sandboxv1alpha1.ConditionReady, metav1.ConditionFalse, "RuntimeApplyFailed", err.Error(), env.Generation)
		_ = c.Status().Update(ctx, &env)
		return ctrl.Result{}, err
	}
	env.Status = status
	if err := c.Status().Update(ctx, &env); err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("DevEnvironment reconciled", "phase", env.Status.Phase, "previewURL", env.Status.PreviewURL)
	return ctrl.Result{}, nil
}

type RuntimeReconciler struct {
	RuntimeClient kubernetes.Interface
	ClusterName   string
}

func (r *RuntimeReconciler) EnsureRuntimeObjects(ctx context.Context, env *sandboxv1alpha1.DevEnvironment) (sandboxv1alpha1.DevEnvironmentStatus, error) {
	if r.RuntimeClient == nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, fmt.Errorf("runtime client is required")
	}
	ns := runtimeNamespace(r.ClusterName)
	if err := r.ensureNamespace(ctx, ns); err != nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, err
	}
	if err := r.ensurePVC(ctx, ns, env); err != nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, err
	}
	if err := r.ensureControlSecret(ctx, ns, env); err != nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, err
	}
	if err := r.ensureDeployment(ctx, ns, env); err != nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, err
	}
	if err := r.ensureService(ctx, ns, env); err != nil {
		return sandboxv1alpha1.DevEnvironmentStatus{}, err
	}
	available := false
	deploy, err := r.RuntimeClient.AppsV1().Deployments(ns).Get(ctx, deploymentName(env.Name), metav1.GetOptions{})
	if err == nil {
		available = deploy.Status.AvailableReplicas >= 1
	}
	status := env.Status
	status.PreviewURL = previewURL(env.Name)
	status.ObservedGeneration = env.Generation
	if available {
		status.Phase = sandboxv1alpha1.DevEnvironmentPhaseRunning
		setCondition(&status.Conditions, sandboxv1alpha1.ConditionReady, metav1.ConditionTrue, "RunnerReady", "runner deployment has available replicas", env.Generation)
	} else {
		status.Phase = sandboxv1alpha1.DevEnvironmentPhaseProvisioning
		setCondition(&status.Conditions, sandboxv1alpha1.ConditionReady, metav1.ConditionFalse, "RunnerProvisioning", "runner deployment is not available yet", env.Generation)
	}
	return status, nil
}

func (r *RuntimeReconciler) ensureNamespace(ctx context.Context, ns string) error {
	want := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
			Labels: map[string]string{
				LabelTenantHash: tenantHash(r.ClusterName),
			},
			Annotations: map[string]string{
				AnnotationLogicalCluster: r.ClusterName,
			},
		},
	}
	existing, err := r.RuntimeClient.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = r.RuntimeClient.CoreV1().Namespaces().Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Labels[LabelTenantHash] = tenantHash(r.ClusterName)
	existing.Annotations[AnnotationLogicalCluster] = r.ClusterName
	_, err = r.RuntimeClient.CoreV1().Namespaces().Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (r *RuntimeReconciler) ensurePVC(ctx context.Context, ns string, env *sandboxv1alpha1.DevEnvironment) error {
	want := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName(env.Name), Namespace: ns, Labels: labelsFor(env.Name)},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	existing, err := r.RuntimeClient.CoreV1().PersistentVolumeClaims(ns).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = r.RuntimeClient.CoreV1().PersistentVolumeClaims(ns).Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Labels = labelsFor(env.Name)
	_, err = r.RuntimeClient.CoreV1().PersistentVolumeClaims(ns).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (r *RuntimeReconciler) ensureControlSecret(ctx context.Context, ns string, env *sandboxv1alpha1.DevEnvironment) error {
	existing, err := r.RuntimeClient.CoreV1().Secrets(ns).Get(ctx, controlSecretName(env.Name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		token, err := randomControlToken()
		if err != nil {
			return err
		}
		_, err = r.RuntimeClient.CoreV1().Secrets(ns).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: controlSecretName(env.Name), Namespace: ns, Labels: labelsFor(env.Name)},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"token": []byte(token)},
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if len(existing.Data["token"]) == 0 {
		token, err := randomControlToken()
		if err != nil {
			return err
		}
		if existing.Data == nil {
			existing.Data = map[string][]byte{}
		}
		existing.Data["token"] = []byte(token)
	}
	existing.Labels = labelsFor(env.Name)
	_, err = r.RuntimeClient.CoreV1().Secrets(ns).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (r *RuntimeReconciler) ensureDeployment(ctx context.Context, ns string, env *sandboxv1alpha1.DevEnvironment) error {
	one := int32(1)
	port := runtimePort(env)
	workdir := runtimeWorkingDir(env)
	want := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: deploymentName(env.Name), Namespace: ns, Labels: labelsFor(env.Name)},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: labelsFor(env.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labelsFor(env.Name)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "runner",
						Image: runtimeImage(env),
						Env: []corev1.EnvVar{
							{Name: "SANDBOX_WORKDIR", Value: workdir},
							{Name: "SANDBOX_START_COMMAND", Value: runtimeStartCommand(env)},
							{Name: "SANDBOX_PORT", Value: strconv.Itoa(int(port))},
							{
								Name: "SANDBOX_CONTROL_TOKEN",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: controlSecretName(env.Name)},
										Key:                  "token",
									},
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{Name: "preview", ContainerPort: port},
							{Name: "control", ContainerPort: 7070},
						},
						VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workdir}},
					}},
					Volumes: []corev1.Volume{{
						Name: "workspace",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName(env.Name)},
						},
					}},
				},
			},
		},
	}
	existing, err := r.RuntimeClient.AppsV1().Deployments(ns).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = r.RuntimeClient.AppsV1().Deployments(ns).Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	want.ResourceVersion = existing.ResourceVersion
	want.Status = existing.Status
	_, err = r.RuntimeClient.AppsV1().Deployments(ns).Update(ctx, want, metav1.UpdateOptions{})
	return err
}

func (r *RuntimeReconciler) ensureService(ctx context.Context, ns string, env *sandboxv1alpha1.DevEnvironment) error {
	port := runtimePort(env)
	want := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName(env.Name), Namespace: ns, Labels: labelsFor(env.Name)},
		Spec: corev1.ServiceSpec{
			Selector: labelsFor(env.Name),
			Ports: []corev1.ServicePort{
				{Name: "preview", Port: port, TargetPort: intstr.FromString("preview")},
				{Name: "control", Port: 7070, TargetPort: intstr.FromString("control")},
			},
		},
	}
	existing, err := r.RuntimeClient.CoreV1().Services(ns).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = r.RuntimeClient.CoreV1().Services(ns).Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	want.ResourceVersion = existing.ResourceVersion
	want.Spec.ClusterIP = existing.Spec.ClusterIP
	want.Spec.ClusterIPs = existing.Spec.ClusterIPs
	want.Spec.IPFamilies = existing.Spec.IPFamilies
	want.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	want.Spec.InternalTrafficPolicy = existing.Spec.InternalTrafficPolicy
	_, err = r.RuntimeClient.CoreV1().Services(ns).Update(ctx, want, metav1.UpdateOptions{})
	return err
}

func runtimeNamespace(clusterName string) string {
	sum := sha256.Sum256([]byte(clusterName))
	return "sandbox-" + hex.EncodeToString(sum[:])[:16]
}

func tenantHash(clusterName string) string {
	sum := sha256.Sum256([]byte(clusterName))
	return hex.EncodeToString(sum[:])[:16]
}

func pvcName(name string) string        { return name + "-workspace" }
func deploymentName(name string) string { return name + "-runner" }
func serviceName(name string) string    { return name + "-preview" }
func controlSecretName(name string) string {
	return name + "-control"
}

func randomControlToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func previewURL(name string) string {
	return "/services/providers/sandbox/api/dev-environments/" + name + "/preview/"
}

func labelsFor(name string) map[string]string {
	return map[string]string{LabelDevEnvironment: name}
}

func runtimePort(env *sandboxv1alpha1.DevEnvironment) int32 {
	if env.Spec.Runtime.Port > 0 {
		return env.Spec.Runtime.Port
	}
	return DefaultRuntimePort
}

func runtimeWorkingDir(env *sandboxv1alpha1.DevEnvironment) string {
	if strings.TrimSpace(env.Spec.Runtime.WorkingDir) != "" {
		return env.Spec.Runtime.WorkingDir
	}
	return DefaultRuntimeWorkDir
}

func runtimeImage(env *sandboxv1alpha1.DevEnvironment) string {
	if strings.TrimSpace(env.Spec.Runtime.Image) != "" {
		return env.Spec.Runtime.Image
	}
	return DefaultRunnerImage
}

func runtimeStartCommand(env *sandboxv1alpha1.DevEnvironment) string {
	if strings.TrimSpace(env.Spec.Runtime.StartCommand) != "" {
		return env.Spec.Runtime.StartCommand
	}
	return DefaultStartCommand
}

func setCondition(conditions *[]metav1.Condition, typ string, status metav1.ConditionStatus, reason, message string, generation int64) {
	now := metav1.Now()
	next := metav1.Condition{
		Type:               typ,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionTime: now,
	}
	for i := range *conditions {
		if (*conditions)[i].Type == typ {
			if (*conditions)[i].Status == status {
				next.LastTransitionTime = (*conditions)[i].LastTransitionTime
			}
			(*conditions)[i] = next
			return
		}
	}
	*conditions = append(*conditions, next)
}

func findCondition(conditions []metav1.Condition, typ string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == typ {
			return &conditions[i]
		}
	}
	return nil
}

func clusterClient(ctx context.Context, mgr mcmanager.Manager, clusterName multicluster.ClusterName) (client.Client, error) {
	cluster, err := mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return cluster.GetClient(), nil
}
