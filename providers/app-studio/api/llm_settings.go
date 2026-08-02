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

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	asclient "github.com/faroshq/provider-app-studio/client"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// LLM settings persistence and validation: reading/writing the tenant
// Kubernetes Secret, provider/base-URL normalization, and API-key checks.

func readProjectLLMSettings(ctx context.Context, c *asclient.Client) (projectLLMSettings, error) {
	settings := defaultProjectLLMSettings()
	secret, err := c.Resource(secretResource, projectLLMSecretNamespace).Get(ctx, projectLLMSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if v := secretDataValue(secret, "provider"); v != "" {
		settings.Provider = v
	}
	if v := secretDataValue(secret, "baseURL"); v != "" {
		settings.BaseURL = v
	}
	if v := secretDataValue(secret, "model"); v != "" {
		settings.Model = v
	}
	settings.APIKey = secretDataValue(secret, "apiKey")
	return settings, nil
}

func writeProjectLLMSettings(ctx context.Context, c *asclient.Client, settings projectLLMSettings) error {
	secret := projectLLMSettingsSecret(settings)
	existing, err := c.Resource(secretResource, projectLLMSecretNamespace).Get(ctx, projectLLMSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = c.Resource(secretResource, projectLLMSecretNamespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	secret.SetResourceVersion(existing.GetResourceVersion())
	_, err = c.Resource(secretResource, projectLLMSecretNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func defaultProjectLLMSettings() projectLLMSettings {
	return projectLLMSettings{
		Provider: defaultProjectLLMProvider,
		BaseURL:  defaultProjectLLMBaseURL,
		Model:    defaultProjectLLMModel,
	}
}

func normalizeProjectLLMSettings(settings *projectLLMSettings) error {
	settings.Provider = strings.TrimSpace(settings.Provider)
	if settings.Provider == "" {
		settings.Provider = defaultProjectLLMProvider
	}
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.BaseURL = strings.TrimSpace(settings.BaseURL)
	if settings.BaseURL == "" {
		settings.BaseURL = defaultProjectLLMBaseURL
	}
	googleCredential, usesGoogleServiceAccount, err := googleServiceAccountCredentialFromJSON(settings.APIKey)
	if err != nil && strings.EqualFold(settings.Provider, projectLLMProviderGoogle) {
		return err
	}
	if strings.EqualFold(settings.Provider, projectLLMProviderGoogle) {
		switch {
		case usesGoogleServiceAccount && isDefaultGoogleBaseURLCandidate(settings.BaseURL):
			settings.BaseURL = defaultProjectLLMGoogleCloudBaseURL(googleCredential.ProjectID)
		case !usesGoogleServiceAccount && isGenericOpenAIBaseURL(settings.BaseURL):
			settings.BaseURL = defaultProjectLLMGoogleBaseURL
		}
	}
	baseURL, err := normalizeLLMBaseURL(settings.BaseURL)
	if err != nil {
		return err
	}
	settings.BaseURL = baseURL
	if err := validateProjectLLMBaseURL(settings.Provider, settings.BaseURL); err != nil {
		return err
	}
	if err := validateProjectLLMAPIKey(settings.Provider, settings.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(settings.Model) == "" {
		return newValidationError("model cannot be empty")
	}
	return nil
}

func validateProjectLLMBaseURL(provider, raw string) error {
	if strings.EqualFold(strings.TrimSpace(provider), projectLLMProviderGoogle) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	path := strings.ToLower(strings.TrimRight(u.Path, "/"))
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return newValidationError("baseURL must be the provider API base URL, not the /chat/completions operation URL; App Studio appends /chat/completions automatically")
	case strings.HasSuffix(path, "/responses"), strings.HasSuffix(path, "/messages"):
		return newValidationError("baseURL must be the provider API base URL, not a model operation URL; App Studio's OpenAI-compatible provider requires a /chat/completions model")
	default:
		return nil
	}
}

func isGenericOpenAIBaseURL(raw string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(raw), "/"), defaultProjectLLMBaseURL)
}

func isDefaultGoogleBaseURLCandidate(raw string) bool {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	return raw == "" ||
		strings.EqualFold(raw, defaultProjectLLMBaseURL) ||
		strings.EqualFold(raw, defaultProjectLLMGoogleBaseURL)
}

func defaultProjectLLMGoogleCloudBaseURL(projectID string) string {
	return "https://aiplatform.googleapis.com"
}

func validateProjectLLMAPIKey(provider, apiKey string) error {
	if !strings.EqualFold(strings.TrimSpace(provider), projectLLMProviderGoogle) {
		return nil
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	if _, _, err := googleServiceAccountCredentialFromJSON(apiKey); err != nil {
		return err
	}
	if _, ok, _ := googleServiceAccountCredentialFromJSON(apiKey); ok {
		return nil
	}
	if looksLikeJWTOrOAuthToken(apiKey) {
		return newValidationError("Google Gemini settings require a Gemini API key string or service-account JSON credential, not an OAuth/JWT token")
	}
	return nil
}

func googleServiceAccountCredentialFromJSON(raw string) (googleServiceAccountCredential, bool, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return googleServiceAccountCredential{}, false, nil
	}
	var credential googleServiceAccountCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return googleServiceAccountCredential{}, true, newValidationError("Google service-account JSON credential is not valid JSON")
	}
	if !strings.EqualFold(strings.TrimSpace(credential.Type), "service_account") &&
		strings.TrimSpace(credential.ClientEmail) == "" &&
		strings.TrimSpace(credential.PrivateKey) == "" {
		return googleServiceAccountCredential{}, true, newValidationError("Google credentials must be a Gemini API key string or a service-account JSON credential")
	}
	missing := []string{}
	if strings.TrimSpace(credential.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if strings.TrimSpace(credential.ClientEmail) == "" {
		missing = append(missing, "client_email")
	}
	if strings.TrimSpace(credential.PrivateKey) == "" {
		missing = append(missing, "private_key")
	}
	if strings.TrimSpace(credential.TokenURI) == "" {
		missing = append(missing, "token_uri")
	}
	if len(missing) > 0 {
		return googleServiceAccountCredential{}, true, newValidationError("Google service-account JSON credential is missing " + strings.Join(missing, ", "))
	}
	return credential, true, nil
}

func looksLikeJWTOrOAuthToken(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "ya29.") {
		return true
	}
	if strings.Count(raw, ".") != 2 {
		return false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var header map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return false
	}
	typ := strings.TrimSpace(fmt.Sprint(header["typ"]))
	_, hasAlg := header["alg"]
	_, hasKeyID := header["kid"]
	return strings.EqualFold(typ, "JWT") || hasAlg || hasKeyID
}

func (s projectLLMSettings) view() ProjectLLMSettingsView {
	return ProjectLLMSettingsView{
		Provider:   s.Provider,
		BaseURL:    s.BaseURL,
		Model:      s.Model,
		Configured: strings.TrimSpace(s.APIKey) != "",
	}
}

func projectLLMSettingsSecret(settings projectLLMSettings) *unstructured.Unstructured {
	data := map[string]interface{}{
		"provider": encodeSecretValue(settings.Provider),
		"baseURL":  encodeSecretValue(settings.BaseURL),
		"model":    encodeSecretValue(settings.Model),
	}
	if strings.TrimSpace(settings.APIKey) != "" {
		data["apiKey"] = encodeSecretValue(settings.APIKey)
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      projectLLMSecretName,
			"namespace": projectLLMSecretNamespace,
		},
		"type": "Opaque",
		"data": data,
	}}
}

func secretDataValue(secret *unstructured.Unstructured, key string) string {
	data, _, _ := unstructured.NestedStringMap(secret.Object, "data")
	if encoded := data[key]; encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil {
			return string(decoded)
		}
	}
	stringData, _, _ := unstructured.NestedStringMap(secret.Object, "stringData")
	return stringData[key]
}

func encodeSecretValue(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func normalizeLLMBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		raw = defaultProjectLLMBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", newValidationError("baseURL must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", newValidationError("baseURL must use http or https")
	}
	u.Path = normalizeLLMBasePath(u.Path, strings.ToLower(u.Host))
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizeLLMBasePath(path string, host string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	if strings.Contains(host, "generativelanguage.googleapis.com") {
		return ""
	}
	if strings.Contains(host, "aiplatform.googleapis.com") {
		lowerPath := strings.ToLower(path)
		if strings.Contains(lowerPath, "/endpoints/openapi") {
			return strings.TrimRight(path[:strings.Index(lowerPath, "/endpoints/openapi")], "/")
		}
	}
	return path
}
