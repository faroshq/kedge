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

package tunnel

// The service catalog turns a handful of popular self-hosted apps into MCP
// tools without bespoke Go per app: each is a data table (auth style + a list
// of HTTP operations), and one generic registrar/executor drives them all.
// Home Assistant stays hand-written (mcp_service.go) because its tools are
// trimmed/reshaped; everything here is a thin, faithful proxy of the app's API.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faroshq/provider-edges/internal/haclient"
)

// snippet trims a byte slice for inclusion in an error message.
func snippet(b []byte) string {
	const max = 240
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// authKind selects how the service's token authenticates a request.
type authKind int

const (
	authBearer         authKind = iota // Authorization: Bearer <token>   (Grafana)
	authAPIKeyHeader                   // <headerName>: <token>            (*arr apps: X-Api-Key)
	authAPIKeyQuery                    // ?<param>=<token>
	authQBittorrent                    // POST /api/v2/auth/login -> SID cookie
)

// catToolInput is the single, generic tool input. A tool's description tells the
// model which of these to fill; reads typically need none.
type catToolInput struct {
	Query map[string]string `json:"query,omitempty" jsonschema:"description=Optional query-string parameters, e.g. {\"query\":\"ubuntu\"} for a search."`
	Form  map[string]string `json:"form,omitempty" jsonschema:"description=Optional form fields for form-encoded APIs (qBittorrent actions), e.g. {\"urls\":\"magnet:?...\"}."`
	Body  string            `json:"body,omitempty" jsonschema:"description=Optional raw JSON request body for JSON POST/PUT actions."`
}

// catTool is one HTTP operation exposed as an MCP tool.
type catTool struct {
	name   string // suffix appended after the service prefix
	desc   string
	method string
	path   string
}

// svcDef is a catalog entry: how to auth + which operations to expose.
type svcDef struct {
	displayName string
	defaultPort int32
	auth        authKind
	authParam   string // header name (authAPIKeyHeader) or query key (authAPIKeyQuery)
	tools       []catTool
}

// svcCatalog is the registry of data-driven service types. Keys are the
// Service.spec.type values. Home Assistant is intentionally absent (hand-coded).
var svcCatalog = map[string]svcDef{
	"prowlarr": {
		displayName: "Prowlarr", defaultPort: 9696, auth: authAPIKeyHeader, authParam: "X-Api-Key",
		tools: []catTool{
			{"indexers", "List configured Prowlarr indexers.", http.MethodGet, "/api/v1/indexer"},
			{"search", "Search across indexers. Pass query params, e.g. {\"query\":\"ubuntu\",\"type\":\"search\"}.", http.MethodGet, "/api/v1/search"},
			{"status", "Prowlarr system status/version.", http.MethodGet, "/api/v1/system/status"},
		},
	},
	"sonarr": {
		displayName: "Sonarr", defaultPort: 8989, auth: authAPIKeyHeader, authParam: "X-Api-Key",
		tools: []catTool{
			{"series", "List all TV series in Sonarr.", http.MethodGet, "/api/v3/series"},
			{"queue", "List the download queue.", http.MethodGet, "/api/v3/queue"},
			{"calendar", "Upcoming/recent episodes (query: start, end as ISO dates).", http.MethodGet, "/api/v3/calendar"},
			{"lookup", "Search for a series to add. Pass query {\"term\":\"the wire\"}.", http.MethodGet, "/api/v3/series/lookup"},
		},
	},
	"radarr": {
		displayName: "Radarr", defaultPort: 7878, auth: authAPIKeyHeader, authParam: "X-Api-Key",
		tools: []catTool{
			{"movies", "List all movies in Radarr.", http.MethodGet, "/api/v3/movie"},
			{"queue", "List the download queue.", http.MethodGet, "/api/v3/queue"},
			{"lookup", "Search for a movie to add. Pass query {\"term\":\"dune 2021\"}.", http.MethodGet, "/api/v3/movie/lookup"},
		},
	},
	"grafana": {
		displayName: "Grafana", defaultPort: 3000, auth: authBearer,
		tools: []catTool{
			{"search", "Search dashboards/folders. Query: {\"query\":\"cpu\",\"type\":\"dash-db\"}.", http.MethodGet, "/api/search"},
			{"datasources", "List configured data sources.", http.MethodGet, "/api/datasources"},
			{"health", "Grafana health/version.", http.MethodGet, "/api/health"},
			{"query", "Run a data-source query. Pass a JSON body per the Grafana /api/ds/query schema.", http.MethodPost, "/api/ds/query"},
		},
	},
	"qbittorrent": {
		displayName: "qBittorrent", defaultPort: 8080, auth: authQBittorrent,
		tools: []catTool{
			{"torrents", "List torrents (filter/category via query, e.g. {\"filter\":\"downloading\"}).", http.MethodGet, "/api/v2/torrents/info"},
			{"transfer", "Global transfer stats (speeds, totals).", http.MethodGet, "/api/v2/transfer/info"},
			{"add", "Add a torrent. Pass form {\"urls\":\"magnet:?...\"}.", http.MethodPost, "/api/v2/torrents/add"},
			{"pause", "Pause torrents. Pass form {\"hashes\":\"<hash>|all\"}.", http.MethodPost, "/api/v2/torrents/pause"},
			{"resume", "Resume torrents. Pass form {\"hashes\":\"<hash>|all\"}.", http.MethodPost, "/api/v2/torrents/resume"},
			{"delete", "Delete torrents. Form {\"hashes\":\"<hash>\",\"deleteFiles\":\"false\"}.", http.MethodPost, "/api/v2/torrents/delete"},
		},
	},
}

// catalogServiceType reports whether a Service type is served by the catalog.
func catalogServiceType(t string) bool {
	_, ok := svcCatalog[t]
	return ok
}

// registerCatalogTools installs the MCP tools for a catalog service type.
func (p *Server) registerCatalogTools(srv *mcp.Server, prefix, cluster, kcpToken string, svc *serviceView, dialer haclient.Dialer) {
	def, ok := svcCatalog[svc.Spec.Type]
	if !ok {
		return
	}
	for _, t := range def.tools {
		tool := t // capture
		mcp.AddTool(srv, &mcp.Tool{
			Name:        prefix + tool.name,
			Description: def.displayName + " — " + tool.desc,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in catToolInput) (*mcp.CallToolResult, any, error) {
			return p.callCatalogTool(ctx, cluster, kcpToken, svc, dialer, def, tool, in)
		})
	}
}

// callCatalogTool executes one catalog tool: resolve the token, apply the auth
// style, build the request (query/form/body), proxy it through the edge, and
// return the response body verbatim.
func (p *Server) callCatalogTool(ctx context.Context, cluster, kcpToken string, svc *serviceView, dialer haclient.Dialer, def svcDef, tool catTool, in catToolInput) (*mcp.CallToolResult, any, error) {
	token, err := p.readServiceToken(ctx, cluster, svc, kcpToken)
	if err != nil {
		return toolErr("service credentials: " + err.Error()), nil, nil
	}
	if token == "" {
		return toolErr("no auth token configured for this service (set spec.authSecretRef)"), nil, nil
	}

	header := http.Header{}
	q := url.Values{}
	for k, v := range in.Query {
		q.Set(k, v)
	}

	switch def.auth {
	case authBearer:
		header.Set("Authorization", "Bearer "+token)
	case authAPIKeyHeader:
		header.Set(def.authParam, token)
	case authAPIKeyQuery:
		q.Set(def.authParam, token)
	case authQBittorrent:
		sid, err := p.qbitLogin(ctx, cluster, svc, dialer, token)
		if err != nil {
			return toolErr("qBittorrent login failed: " + err.Error()), nil, nil
		}
		header.Set("Cookie", "SID="+sid)
		header.Set("Referer", (haclient.Target{Scheme: svc.scheme(), Host: svc.targetHost(), Port: svc.Spec.Port}).SvcTarget())
	}

	// Request body: form-encoded (qBittorrent actions) or raw JSON.
	var bodyReader io.Reader
	switch {
	case len(in.Form) > 0:
		form := url.Values{}
		for k, v := range in.Form {
			form.Set(k, v)
		}
		bodyReader = strings.NewReader(form.Encode())
		header.Set("Content-Type", "application/x-www-form-urlencoded")
	case strings.TrimSpace(in.Body) != "":
		bodyReader = strings.NewReader(in.Body)
		header.Set("Content-Type", "application/json")
	}

	path := tool.path
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	target := haclient.Target{Scheme: svc.scheme(), Host: svc.targetHost(), Port: svc.Spec.Port}
	resp, err := haclient.DoWith(ctx, dialer, target, tool.method, path, header, bodyReader)
	if err != nil {
		return toolErr(err.Error()), nil, nil
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return toolErr(fmt.Sprintf("%s returned %d: %s", def.displayName, resp.StatusCode, snippet(respBody))), nil, nil
	}
	text := string(respBody)
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("OK (%d)", resp.StatusCode)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

// qbitLogin performs qBittorrent's cookie login and returns the SID. The token
// Secret holds "username:password" (the WebUI credentials). Returns an error if
// the app rejects the login (wrong creds, or CSRF/host-header protection — in
// which case whitelist the edge or disable those checks in qBittorrent).
func (p *Server) qbitLogin(ctx context.Context, cluster string, svc *serviceView, dialer haclient.Dialer, cred string) (string, error) {
	user, pass, ok := strings.Cut(cred, ":")
	if !ok {
		return "", fmt.Errorf("qBittorrent credential must be \"username:password\"")
	}
	form := url.Values{"username": {user}, "password": {pass}}
	target := haclient.Target{Scheme: svc.scheme(), Host: svc.targetHost(), Port: svc.Spec.Port}
	header := http.Header{
		"Content-Type": {"application/x-www-form-urlencoded"},
		"Referer":      {target.SvcTarget()},
	}
	resp, err := haclient.DoWith(ctx, dialer, target, http.MethodPost, "/api/v2/auth/login", header, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), "Fails") {
		return "", fmt.Errorf("login rejected (%d): %s", resp.StatusCode, snippet(body))
	}
	for _, c := range resp.Cookies() {
		if c.Name == "SID" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("no SID cookie in login response")
}
