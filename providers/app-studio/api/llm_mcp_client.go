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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Minimal MCP JSON-RPC client used to broker tenant MCP tools to the
// assistant: endpoint resolution, tools/list + tools/call, TLS fallback.

func (s *Server) loadProjectMCPTools(r *http.Request, id identity, settings projectLLMSettings) ([]chatTool, error) {
	if id.tenantPath == "" {
		return nil, errors.New("tenant context missing")
	}
	registry := s.projectAssistantToolRegistry()
	out := registry.ChatTools(false)
	mcpTools, codeCommitAvailable, err := s.loadProjectMCPAssistantTools(r, id, settings)
	if err != nil {
		return out, err
	}
	if codeCommitAvailable {
		if tool, ok := registry.ChatTool(projectToolCommitProjectFiles); ok {
			out = append(out, tool)
		}
	}
	for _, tool := range mcpTools {
		out = append(out, tool.Spec().chatTool())
	}
	return out, nil
}

func (s *Server) loadProjectMCPAssistantTools(r *http.Request, id identity, _ projectLLMSettings) ([]projectAssistantTool, bool, error) {
	if id.tenantPath == "" {
		return nil, false, errors.New("tenant context missing")
	}
	if id.clusterID == "" {
		return nil, false, errors.New("no workspace cluster on request (X-Kedge-Cluster missing) — cannot address the tenant MCP endpoint")
	}
	mcpEndpoint := s.mcpEndpoint(id.clusterID)
	tools, err := fetchProjectMCPTools(r.Context(), mcpEndpoint, r, id.tenantPath, s.mcpInsecureSkipTLSVerify)
	if err != nil {
		return nil, false, err
	}
	codeCommitAvailable := false
	for _, t := range tools {
		if projectMCPCommitToolAvailable(t.Name) {
			codeCommitAvailable = true
			break
		}
	}
	return projectAssistantMCPToolsForSpecs(tools, s.mcpInsecureSkipTLSVerify), codeCommitAvailable, nil
}

// mcpEndpoint returns the hub's unified MCPServer virtual-workspace endpoint for
// the given tenant logical-cluster ID. The provider always reaches MCP through
// the hub (KEDGE_HUB_URL), not its own host. The workspace MUST be addressed by
// logical-cluster ID (the hub-injected X-Kedge-Cluster), never by workspace
// path: the hub proxy's membership gate rejects path-form /clusters/<root:...>
// addressing with a 403 ("address workspaces by cluster ID, not by path").
func (s *Server) mcpEndpoint(clusterID string) string {
	return mcpServerURL(s.hubBase, clusterID, "default")
}

// mcpServerURL mirrors pkg/apiurl.MCPServerURL in the kedge monorepo:
// {hub}/services/mcpserver/{cluster}/apis/kedge.faros.sh/v1alpha1/mcpservers/{name}/mcp
// cluster is the workspace's logical-cluster ID, never its path.
func mcpServerURL(hubBase, cluster, mcpServerName string) string {
	return strings.TrimRight(hubBase, "/") +
		fmt.Sprintf("/services/mcpserver/%s/apis/kedge.faros.sh/v1alpha1/mcpservers/%s/mcp", cluster, mcpServerName)
}

func fetchProjectMCPTools(ctx context.Context, endpoint string, r *http.Request, tenantPath string, skipTLSVerify bool) ([]projectMCPTool, error) {
	params := []byte(`{}`)
	body, err := projectMCPRequest(ctx, endpoint, "tools/list", params, r, tenantPath, skipTLSVerify)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Tools []projectMCPTool `json:"tools"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode tools/list response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("provider MCP error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Tools, nil
}

func callProjectMCPTool(ctx context.Context, endpoint string, r *http.Request, tenantPath string, skipTLSVerify bool, name string, args map[string]any) (string, error) {
	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", fmt.Errorf("encode tool args: %w", err)
	}
	body, err := projectMCPRequest(ctx, endpoint, "tools/call", params, r, tenantPath, skipTLSVerify)
	if err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
		IsError           bool            `json:"isError,omitempty"`
		ErrorMessage      string          `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err == nil {
		textParts := make([]string, 0, len(result.Content))
		for _, item := range result.Content {
			if item.Type == "text" && item.Text != "" {
				textParts = append(textParts, item.Text)
			}
		}
		if result.IsError {
			if result.ErrorMessage != "" {
				return "", errors.New(result.ErrorMessage)
			}
			if len(textParts) > 0 {
				return "", errors.New(strings.Join(textParts, "\n"))
			}
			if len(result.StructuredContent) > 0 {
				return "", errors.New(string(result.StructuredContent))
			}
			return "", errors.New("tool call returned an error")
		}
		if len(textParts) > 0 {
			return strings.Join(textParts, "\n"), nil
		}
		if len(result.StructuredContent) > 0 {
			return string(result.StructuredContent), nil
		}
	}
	return string(body), nil
}

func projectMCPRequest(ctx context.Context, endpoint, method string, paramsJSON json.RawMessage, r *http.Request, tenantPath string, skipTLSVerify bool) (json.RawMessage, error) {
	env := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  json.RawMessage(paramsJSON),
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode MCP request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("new MCP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth := r.Header.Get("Authorization"); strings.TrimSpace(auth) != "" {
		req.Header.Set("Authorization", auth)
	}
	if tenantPath != "" {
		req.Header.Set("X-Kedge-Tenant", tenantPath)
	}

	transport := projectMCPTransport(skipTLSVerify)
	client := &http.Client{Timeout: projectMCPCallTimeout, Transport: transport}
	resp, err := client.Do(req)
	if err != nil && projectMCPShouldRetryInsecure(endpoint, err, skipTLSVerify) {
		transport = projectMCPTransport(true)
		client = &http.Client{Timeout: projectMCPCallTimeout, Transport: transport}
		resp, err = client.Do(req)
	}
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read MCP body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	raw := body
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		parsed, ok := firstSSELine(raw)
		if !ok {
			return nil, errors.New("MCP response had no SSE data")
		}
		raw = parsed
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode MCP JSON-RPC envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("provider MCP error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

func projectMCPTransport(insecureSkipVerify bool) http.RoundTripper {
	if !insecureSkipVerify {
		return http.DefaultTransport
	}

	if baseTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := baseTransport.Clone()
		clone.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // dev-only
		return clone
	}

	return &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // dev-only
}

func projectMCPShouldRetryInsecure(endpoint string, err error, skipTLSVerify bool) bool {
	if skipTLSVerify {
		return false
	}
	if !isLocalhostEndpointForMCP(endpoint) {
		return false
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		var unknownAuthority x509.UnknownAuthorityError
		if errors.As(certErr.Err, &unknownAuthority) {
			return true
		}
	}
	var unknownAuthority *x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var certInvalid *x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return true
	}
	var hostErr *x509.HostnameError
	if errors.As(err, &hostErr) {
		return true
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unknown certificate authority") ||
		strings.Contains(errMsg, "certificate verification") ||
		strings.Contains(errMsg, "bad certificate") ||
		strings.Contains(errMsg, "certificate is not valid")
}

func isLocalhostEndpointForMCP(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

func firstSSELine(body []byte) (json.RawMessage, bool) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			return json.RawMessage(strings.TrimPrefix(line, "data: ")), true
		}
	}
	return nil, false
}
