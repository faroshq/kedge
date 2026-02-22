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

package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// kcp helm chart reference
	kcpHelmRepo     = "kcp-dev"
	kcpHelmRepoURL  = "https://kcp-dev.github.io/helm-charts"
	kcpChartRef     = "kcp-dev/kcp"
	kcpReleaseName  = "kcp"
	kcpNamespace    = "kcp"
	kcpChartVersion = "0.14.0" // matches kcp app version v0.30.0

	// kcp networking: front-proxy service port and NodePort
	// externalPort=8443 → ClusterIP service port
	// nodePort=30643   → NodePort on kind node
	// kind extraPortMapping: containerPort=30643, hostPort=KCPHTTPSPort(7443)
	kcpExternalPort = 8443
	kcpNodePort     = 30643

	// kcp external hostname — the in-cluster DNS name of the front-proxy service.
	// Using the in-cluster service name as externalHostname means the TLS cert
	// is valid for in-cluster access without needing hostAliases on the hub pod.
	kcpExternalHostname = "kcp-front-proxy.kcp.svc.cluster.local"

	// cert-manager version for kcp TLS
	certManagerVersion = "v1.17.2"

	// kcp admin certificate (issued by kcp's cert-manager Issuer)
	kcpAdminCertName   = "kedge-e2e-admin"
	kcpAdminSecretName = "kedge-e2e-admin"

	// Secret name for kcp admin kubeconfig (mounted into hub pod)
	kcpAdminKubeconfigSecret = "kcp-admin-kubeconfig"

	// File name for the external kcp kubeconfig written to the working directory
	kcpExternalKubeconfigFile = "kcp-admin.kubeconfig"
)

// ensureKCPHelmRepo adds the kcp-dev helm repo if it isn't already present.
func ensureKCPHelmRepo() error {
	addCmd := exec.Command("helm", "repo", "add", kcpHelmRepo, kcpHelmRepoURL)
	out, err := addCmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "already exists") {
		return fmt.Errorf("adding kcp-dev helm repo: %w\noutput: %s", err, string(out))
	}
	updateCmd := exec.Command("helm", "repo", "update", kcpHelmRepo)
	if out, err := updateCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("updating kcp-dev helm repo: %w\noutput: %s", err, string(out))
	}
	return nil
}

// ensureCertManager installs cert-manager into the cluster and waits for it
// to be ready. Idempotent — safe to call on an existing cert-manager install.
func ensureCertManager(ctx context.Context, kubeconfigPath string) error {
	url := fmt.Sprintf(
		"https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml",
		certManagerVersion,
	)
	applyCmd := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", kubeconfigPath,
		"apply", "--server-side", "-f", url,
	)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("applying cert-manager manifests: %w", err)
	}

	waitCmd := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", kubeconfigPath,
		"wait", "--for=condition=Available",
		"deployment", "--all",
		"-n", "cert-manager",
		"--timeout=5m",
	)
	waitCmd.Stdout = os.Stdout
	waitCmd.Stderr = os.Stderr
	if err := waitCmd.Run(); err != nil {
		return fmt.Errorf("waiting for cert-manager to be ready: %w", err)
	}
	return nil
}

// deployKCPViaHelm installs the kcp Helm chart into the hub kind cluster and
// waits for the front-proxy pod to be ready.
func (o *DevOptions) deployKCPViaHelm(ctx context.Context, restConfig *rest.Config) error {
	if err := ensureKCPHelmRepo(); err != nil {
		return fmt.Errorf("ensuring kcp helm repo: %w", err)
	}

	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(&restConfigGetter{config: restConfig}, kcpNamespace, "secret",
		func(format string, v ...any) {}); err != nil {
		return fmt.Errorf("initialising helm action config for kcp: %w", err)
	}
	regClient, err := registry.NewClient()
	if err != nil {
		return fmt.Errorf("creating helm registry client for kcp: %w", err)
	}
	actionConfig.RegistryClient = regClient

	kcpValues := map[string]any{
		"externalHostname": kcpExternalHostname,
		"externalPort": fmt.Sprintf("%d", kcpExternalPort),
		"kcpFrontProxy": map[string]any{
			"service": map[string]any{
				"type":     "NodePort",
				"nodePort": kcpNodePort,
			},
		},
		"audit": map[string]any{
			"enabled": false,
		},
	}

	tmp := action.NewInstall(actionConfig)
	tmp.Version = kcpChartVersion
	chartPath, err := tmp.LocateChart(kcpChartRef, cli.New())
	if err != nil {
		return fmt.Errorf("locating kcp chart: %w", err)
	}
	chartObj, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("loading kcp chart: %w", err)
	}

	hist := action.NewHistory(actionConfig)
	hist.Max = 1
	if _, err := hist.Run(kcpReleaseName); err == nil {
		upg := action.NewUpgrade(actionConfig)
		upg.Namespace = kcpNamespace
		upg.Wait = true
		upg.Timeout = 8 * time.Minute
		if _, err := upg.Run(kcpReleaseName, chartObj, kcpValues); err != nil {
			return fmt.Errorf("upgrading kcp chart: %w", err)
		}
	} else {
		inst := action.NewInstall(actionConfig)
		inst.ReleaseName = kcpReleaseName
		inst.Namespace = kcpNamespace
		inst.CreateNamespace = true
		inst.Wait = true
		inst.Timeout = 8 * time.Minute
		if _, err := inst.Run(chartObj, kcpValues); err != nil {
			return fmt.Errorf("installing kcp chart: %w", err)
		}
	}

	return nil
}

// buildKCPKubeconfigs creates an admin client certificate via cert-manager,
// extracts credentials from kcp, and produces two kubeconfigs:
//   - an in-cluster kubeconfig stored as a Kubernetes Secret (for the hub pod)
//   - an external kubeconfig written to workDir/kcp-admin.kubeconfig (for tests)
func (o *DevOptions) buildKCPKubeconfigs(ctx context.Context, restConfig *rest.Config, kubeconfigPath, workDir string) error {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	// --- 1. Extract kcp CA certificate ---
	caSecret, err := clientset.CoreV1().Secrets(kcpNamespace).Get(ctx, "kcp-ca", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting kcp-ca secret: %w", err)
	}
	caCert := caSecret.Data["tls.crt"]
	if len(caCert) == 0 {
		return fmt.Errorf("kcp-ca secret has no tls.crt field")
	}

	// --- 2. Apply admin client Certificate resource ---
	certYAML := fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: %s
  namespace: %s
spec:
  commonName: kedge-e2e-admin
  issuerRef:
    name: kcp-front-proxy-client-issuer
    kind: Issuer
  secretName: %s
  privateKey:
    algorithm: RSA
    size: 2048
  usages:
    - client auth
  subject:
    organizations:
      - system:kcp:admin
`, kcpAdminCertName, kcpNamespace, kcpAdminSecretName)

	applyCmd := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", kubeconfigPath,
		"apply", "-f", "-",
	)
	applyCmd.Stdin = strings.NewReader(certYAML)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("applying kcp admin Certificate: %w", err)
	}

	// --- 3. Wait for Certificate to be Ready ---
	waitCmd := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", kubeconfigPath,
		"wait", "--for=condition=Ready",
		fmt.Sprintf("certificate/%s", kcpAdminCertName),
		"-n", kcpNamespace,
		"--timeout=3m",
	)
	waitCmd.Stdout = os.Stdout
	waitCmd.Stderr = os.Stderr
	if err := waitCmd.Run(); err != nil {
		return fmt.Errorf("waiting for kcp admin Certificate to be ready: %w", err)
	}

	// --- 4. Extract client cert and key ---
	certSecret, err := clientset.CoreV1().Secrets(kcpNamespace).Get(ctx, kcpAdminSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting kcp admin cert secret: %w", err)
	}
	clientCert := certSecret.Data["tls.crt"]
	clientKey := certSecret.Data["tls.key"]
	if len(clientCert) == 0 || len(clientKey) == 0 {
		return fmt.Errorf("kcp admin cert secret missing tls.crt or tls.key")
	}

	// --- 5. Build in-cluster kubeconfig (for hub pod) ---
	inClusterServer := fmt.Sprintf("https://%s:%d/clusters/root", kcpExternalHostname, kcpExternalPort)
	inClusterKubeconfig := buildKubeconfigWithCerts(inClusterServer, caCert, clientCert, clientKey, false)
	inClusterBytes, err := clientcmd.Write(*inClusterKubeconfig)
	if err != nil {
		return fmt.Errorf("serialising in-cluster kcp kubeconfig: %w", err)
	}

	// --- 6. Build external kubeconfig (for test runner) ---
	externalServer := fmt.Sprintf("https://127.0.0.1:%d/clusters/root", o.KCPHTTPSPort)
	externalKubeconfig := buildKubeconfigWithCerts(externalServer, nil, clientCert, clientKey, true)
	externalBytes, err := clientcmd.Write(*externalKubeconfig)
	if err != nil {
		return fmt.Errorf("serialising external kcp kubeconfig: %w", err)
	}

	// --- 7. Write external kubeconfig to workDir ---
	externalKubeconfigPath := fmt.Sprintf("%s/%s", workDir, kcpExternalKubeconfigFile)
	if err := os.WriteFile(externalKubeconfigPath, externalBytes, 0o600); err != nil {
		return fmt.Errorf("writing external kcp kubeconfig: %w", err)
	}

	// --- 8. Ensure kedge-system namespace exists ---
	_, err = clientset.CoreV1().Namespaces().Get(ctx, "kedge-system", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kedge-system"}}
		if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating kedge-system namespace: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("checking kedge-system namespace: %w", err)
	}

	// --- 9. Create (or update) kcp-admin-kubeconfig Secret in kedge-system ---
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcpAdminKubeconfigSecret,
			Namespace: "kedge-system",
		},
		Data: map[string][]byte{
			"admin.kubeconfig": inClusterBytes,
		},
	}
	_, err = clientset.CoreV1().Secrets("kedge-system").Get(ctx, kcpAdminKubeconfigSecret, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := clientset.CoreV1().Secrets("kedge-system").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("creating kcp-admin-kubeconfig secret: %w", err)
		}
	} else if err == nil {
		if _, err := clientset.CoreV1().Secrets("kedge-system").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating kcp-admin-kubeconfig secret: %w", err)
		}
	} else {
		return fmt.Errorf("checking kcp-admin-kubeconfig secret: %w", err)
	}

	return nil
}

// buildKubeconfigWithCerts builds a kubeconfig using client certificates.
// If caCert is nil, InsecureSkipVerify is used instead.
func buildKubeconfigWithCerts(server string, caCert, clientCert, clientKey []byte, insecure bool) *clientcmdapi.Config {
	cfg := clientcmdapi.NewConfig()

	cluster := &clientcmdapi.Cluster{
		Server: server,
	}
	if insecure {
		cluster.InsecureSkipTLSVerify = true
	} else if len(caCert) > 0 {
		cluster.CertificateAuthorityData = caCert
	}

	cfg.Clusters["kcp"] = cluster
	cfg.AuthInfos["kcp-admin"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: clientCert,
		ClientKeyData:         clientKey,
	}
	cfg.Contexts["kcp"] = &clientcmdapi.Context{
		Cluster:  "kcp",
		AuthInfo: "kcp-admin",
	}
	cfg.CurrentContext = "kcp"
	return cfg
}

// installHelmChartWithExternalKCP installs or upgrades the kedge-hub Helm chart
// with external kcp configuration.
func (o *DevOptions) installHelmChartWithExternalKCP(ctx context.Context, restConfig *rest.Config) error {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(&restConfigGetter{config: restConfig}, "kedge-system", "secret",
		func(format string, v ...any) {}); err != nil {
		return fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	registryClient, err := registry.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}
	actionConfig.RegistryClient = registryClient

	hubExternalURL := fmt.Sprintf("https://kedge.localhost:%d", o.HubHTTPSPort)

	values := map[string]any{
		"image": map[string]any{
			"hub": map[string]any{
				"repository": o.Image,
				"tag":        o.Tag,
				"pullPolicy": o.ImagePullPolicy,
			},
		},
		"hub": map[string]any{
			"hubExternalURL":   hubExternalURL,
			"listenAddr":       fmt.Sprintf(":%d", o.HubHTTPSPort),
			"devMode":          true,
			"staticAuthTokens": []string{"dev-token"},
		},
		"kcp": map[string]any{
			"embedded": map[string]any{
				"enabled": false,
			},
			"external": map[string]any{
				"enabled":        true,
				"existingSecret": kcpAdminKubeconfigSecret,
			},
		},
		"service": map[string]any{
			"type": "NodePort",
			"hub": map[string]any{
				"port":     o.HubHTTPSPort,
				"nodePort": 31443,
			},
		},
	}

	var chartObj *chart.Chart
	var loadErr error
	if strings.HasPrefix(o.ChartPath, "oci://") {
		tmp := action.NewInstall(actionConfig)
		tmp.Version = o.ChartVersion
		chartPath, err := tmp.LocateChart(o.ChartPath, cli.New())
		if err != nil {
			return fmt.Errorf("failed to locate OCI chart: %w", err)
		}
		chartObj, loadErr = loader.Load(chartPath)
	} else {
		chartObj, loadErr = loader.Load(o.ChartPath)
	}
	if loadErr != nil {
		return fmt.Errorf("failed to load chart: %w", loadErr)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run("kedge-hub"); err == nil {
		upg := action.NewUpgrade(actionConfig)
		upg.Namespace = "kedge-system"
		upg.Wait = true
		upg.Timeout = o.WaitForReadyTimeout
		if _, err := upg.Run("kedge-hub", chartObj, values); err != nil {
			return fmt.Errorf("failed to upgrade chart: %w", err)
		}
	} else {
		inst := action.NewInstall(actionConfig)
		inst.ReleaseName = "kedge-hub"
		inst.Namespace = "kedge-system"
		inst.CreateNamespace = false // namespace already created in buildKCPKubeconfigs
		inst.Wait = true
		inst.Timeout = o.WaitForReadyTimeout
		if _, err := inst.Run(chartObj, values); err != nil {
			return fmt.Errorf("failed to install chart: %w", err)
		}
	}

	return nil
}
