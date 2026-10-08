package api

import (
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/reliability"
)

type apiSchema = map[string]any

// ManagementOpenAPI describes the same explicit route policy enforced for tokens.
// Request field names are reflected from handler input types; semantic constraints
// that live in validators are specified below rather than inferred from zero values.
func ManagementOpenAPI() map[string]any {
	paths := map[string]any{}
	for _, route := range ManagementAPIRoutes() {
		item, ok := paths[route.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[route.Path] = item
		}
		resource, _, _ := strings.Cut(route.Scope, ":")
		op := map[string]any{
			"summary": route.Summary, "tags": []string{resource},
			"operationId":      strings.ToLower(route.Method) + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(route.Path),
			"x-required-scope": route.Scope, "x-owner-only": route.OwnerOnly,
			"description": "Requires scope `" + route.Scope + "` and the current account role allowed by this endpoint. Write scopes do not imply read scopes.",
			"responses":   openAPIResponses(route),
		}
		if route.OwnerOnly {
			op["description"] = op["description"].(string) + " Only owner accounts can call this operation."
		}
		parameters := openAPIParameters(route)
		if len(parameters) > 0 {
			op["parameters"] = parameters
		}
		if body := openAPIRequest(route); body != nil {
			op["requestBody"] = body
		}
		item[strings.ToLower(route.Method)] = op
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title": "TGProxy Panel management API", "version": "1",
			"description": "Personal API tokens automate your own installed panel. Create a token in Settings → API tokens. The bearer secret is shown once. Expired or revoked tokens return 401; missing scopes or insufficient current account roles return 403. Administrator management, passwords, TOTP, backups, personal token management and subscription service credentials require browser sessions and are not included in this automation contract.",
		},
		"servers":  []any{map[string]any{"url": "https://{panelHost}", "description": "Replace with your installed panel hostname; tgproxypanel.com hosts documentation only.", "variables": map[string]any{"panelHost": map[string]any{"default": "panel.example.com"}}}},
		"security": []any{map[string]any{"personalToken": []string{}}},
		"paths":    paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{"personalToken": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "tgwp_ opaque token", "description": "Use Authorization: Bearer <token>. Valid personal tokens do not require cookies or X-CSRF-Token. Never put tokens in URLs."}},
			"schemas":         map[string]any{"Error": openAPISchema(reflect.TypeOf(errorBody{}))},
		},
		"externalDocs": map[string]any{"url": "https://tgproxypanel.com/en/api/", "description": "Quick start, permissions, examples and endpoint catalog (RU/EN)."},
	}
}

func openAPISchema(t reflect.Type) apiSchema {
	if t.Kind() == reflect.Pointer {
		s := openAPISchema(t.Elem())
		s["nullable"] = true
		return s
	}
	if t == reflect.TypeOf(time.Time{}) {
		return apiSchema{"type": "string", "format": "date-time"}
	}
	if t == reflect.TypeOf(uuid.UUID{}) {
		return apiSchema{"type": "string", "format": "uuid"}
	}
	if t.PkgPath() == "encoding/json" && t.Name() == "RawMessage" {
		return apiSchema{}
	}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			props[name] = openAPISchema(f.Type)
		}
		return apiSchema{"type": "object", "properties": props}
	case reflect.Map:
		return apiSchema{"type": "object", "additionalProperties": openAPISchema(t.Elem())}
	case reflect.Slice, reflect.Array:
		return apiSchema{"type": "array", "items": openAPISchema(t.Elem())}
	case reflect.Bool:
		return apiSchema{"type": "boolean"}
	case reflect.Float32, reflect.Float64:
		return apiSchema{"type": "number"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return apiSchema{"type": "integer", "format": "int64"}
	case reflect.String:
		return apiSchema{"type": "string"}
	default:
		return apiSchema{}
	}
}

func apiObject(fields map[string]any, required ...string) apiSchema {
	s := apiSchema{"type": "object", "properties": fields}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func apiArray(item apiSchema) apiSchema { return apiSchema{"type": "array", "items": item} }

func apiString() apiSchema { return apiSchema{"type": "string"} }

func openAPIRequest(route APIRoute) map[string]any {
	key := route.Method + " " + strings.TrimPrefix(route.Path, "/api/v1")
	var schema apiSchema
	media := "application/json"
	switch key {
	case "POST /nodes":
		schema = openAPISchema(reflect.TypeOf(createNodeReq{}))
		schema["required"] = []string{"name", "hostname", "acme_email"}
		schema["properties"].(map[string]any)["engine"] = apiSchema{"type": "string", "enum": []string{"telemt", "tproxy"}, "default": "telemt"}
		schema["properties"].(map[string]any)["classic_port"] = apiSchema{"type": "integer", "minimum": 1024, "maximum": 65535, "default": 8443}
	case "PATCH /nodes/{id}":
		schema = openAPISchema(reflect.TypeOf(patchNodeReq{}))
	case "PUT /nodes/order":
		schema = openAPISchema(reflect.TypeOf(reorderNodesReq{}))
		schema["required"] = []string{"ids"}
	case "POST /nodes/preflight/dns":
		schema = apiObject(map[string]any{"hostname": apiString(), "public_ip": apiString()}, "hostname")
	case "PUT /nodes/{id}/web-policy":
		schema = openAPISchema(reflect.TypeOf(domain.WebPolicy{}))
		schema["description"] = "Partial update: omitted fields preserve the current policy; validation uses the merged policy."
	case "PUT /nodes/{id}/reliability":
		schema = openAPISchema(reflect.TypeOf(reliability.Policy{}))
	case "POST /nodes/{id}/telemt-update":
		schema = openAPISchema(reflect.TypeOf(telemtUpdateBody{}))
	case "POST /nodes/{id}/web/lifecycle":
		schema = apiObject(map[string]any{"action": apiSchema{"type": "string", "enum": []string{"pause", "drain", "resume", "reset_learning"}}, "timeout_secs": apiSchema{"type": "integer", "minimum": 1, "maximum": 3600, "default": 120}}, "action")
	case "PUT /nodes/{id}/blocklist":
		schema = apiObject(map[string]any{"revision": apiSchema{"type": "integer", "format": "int64", "nullable": true}, "entries": apiArray(apiObject(map[string]any{"prefix": apiString(), "note": apiString()}, "prefix"))}, "entries")
	case "POST /fleet/updates":
		schema = apiObject(map[string]any{"node_ids": apiSchema{"type": "array", "items": apiSchema{"type": "string", "format": "uuid"}, "minItems": 1, "maxItems": 100}}, "node_ids")
	case "POST /keys", "POST /keys/batch", "POST /keys/import":
		schema = openAPISchema(reflect.TypeOf(keyInput{}))
		props := schema["properties"].(map[string]any)
		props["type"] = apiSchema{"type": "string", "enum": []string{"PERSONAL", "SHARED"}}
		props["node_ids"].(apiSchema)["minItems"] = 1
		props["carrier_mode"] = apiSchema{"type": "string", "enum": domain.AllCarrierModes, "default": "https"}
		schema["required"] = []string{"label", "type", "node_ids"}
		if key != "POST /keys/batch" {
			delete(props, "count")
			delete(props, "prefix")
		} else {
			delete(props, "label")
			schema["required"] = []string{"prefix", "count", "type", "node_ids"}
		}
		if key == "POST /keys/import" {
			delete(props, "label")
			props["items"] = apiArray(apiObject(map[string]any{"label": apiString(), "owner_label": apiString(), "secret": apiString()}, "label", "secret"))
			schema["required"] = []string{"items", "type", "node_ids"}
		}
	case "PATCH /keys/{id}":
		props := openAPISchema(reflect.TypeOf(keyInput{}))["properties"].(map[string]any)
		for _, field := range []string{"type", "node_ids", "prefix", "count"} {
			delete(props, field)
		}
		props["clear_expiry"] = apiSchema{"type": "boolean", "description": "true removes the expiration; null expires_at alone preserves it."}
		schema = apiObject(props)
	case "POST /keys/{id}/bindings":
		schema = apiObject(map[string]any{"node_id": apiSchema{"type": "string", "format": "uuid"}}, "node_id")
	case "POST /keys/bulk":
		schema = apiObject(map[string]any{"action": apiSchema{"type": "string", "enum": []string{"revoke", "delete", "disable", "enable", "extend"}}, "ids": apiSchema{"type": "array", "items": apiSchema{"type": "string", "format": "uuid"}, "minItems": 1, "maxItems": 500}, "expires_at": apiSchema{"type": "string", "format": "date-time", "description": "Required for extend."}}, "action", "ids")
	case "POST /alerts/resolve":
		schema = apiObject(map[string]any{"ids": apiArray(apiSchema{"type": "integer", "format": "int64"})}, "ids")
	case "POST /site-templates", "POST /site-templates/validate", "PUT /site-templates/{id}":
		schema = openAPISchema(reflect.TypeOf(templateInput{}))
		schema["required"] = []string{"html"}
		if key == "POST /site-templates" {
			schema["required"] = []string{"name", "html"}
		}
		schema["properties"].(map[string]any)["assets"].(apiSchema)["description"] = "Map of asset paths to base64-encoded content."
	case "POST /site-templates/import":
		media = "multipart/form-data"
		schema = apiObject(map[string]any{"name": apiString(), "file": apiSchema{"type": "string", "format": "binary", "description": "ZIP bundle"}}, "name", "file")
	case "POST /site-templates/{id}/customize":
		schema = apiObject(map[string]any{"name": apiString(), "variables": apiSchema{"type": "object", "additionalProperties": apiString()}}, "name")
	case "POST /nodes/{id}/site":
		schema = apiObject(map[string]any{"template_id": apiSchema{"type": "string", "format": "uuid"}}, "template_id")
	case "POST /nodes/{id}/site/upstream":
		schema = apiObject(map[string]any{"origin": apiString()}, "origin")
	case "POST /branding/profiles":
		schema = apiObject(map[string]any{"name": apiString()}, "name")
	case "PUT /branding/profiles/{id}":
		props := map[string]any{}
		for _, name := range []string{"name", "panel_name", "primary_color", "accent_color", "theme_default", "login_text", "support_link", "footer_text", "custom_css"} {
			props[name] = apiString()
		}
		schema = apiObject(props)
	case "POST /branding/profiles/{id}/upload":
		media = "multipart/form-data"
		schema = apiObject(map[string]any{"file": apiSchema{"type": "string", "format": "binary"}}, "file")
	case "PUT /settings":
		schema = openAPISchema(reflect.TypeOf(putSettingsReq{}))
	case "POST /settings/telegram/test":
		schema = openAPISchema(reflect.TypeOf(postTelegramTestReq{}))
	}
	if schema == nil {
		return nil
	}
	return map[string]any{"required": true, "content": map[string]any{media: map[string]any{"schema": schema}}}
}

var apiPathParameter = regexp.MustCompile(`\{([^}]+)\}`)

func openAPIParameters(route APIRoute) []any {
	params := []any{}
	for _, match := range apiPathParameter.FindAllStringSubmatch(route.Path, -1) {
		schema := apiSchema{"type": "string", "format": "uuid"}
		if strings.HasPrefix(route.Path, "/api/v1/alerts/") {
			schema = apiSchema{"type": "integer", "format": "int64"}
		}
		params = append(params, map[string]any{"name": match[1], "in": "path", "required": true, "schema": schema})
	}
	query := func(name string, schema apiSchema, description string) {
		params = append(params, map[string]any{"name": name, "in": "query", "schema": schema, "description": description})
	}
	if route.Method != "GET" && !strings.HasSuffix(route.Path, "/upload") {
		return params
	}
	path := strings.TrimPrefix(route.Path, "/api/v1")
	if path == "/keys" || path == "/audit" {
		query("page", apiSchema{"type": "integer", "minimum": 1, "default": 1}, "One-based page number.")
		query("per_page", apiSchema{"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "Invalid values fall back to 50.")
	}
	if path == "/keys" {
		for _, name := range []string{"q", "type", "status", "state", "node"} {
			query(name, apiString(), "User filter; node is a node UUID; type is PERSONAL or SHARED.")
		}
	}
	if path == "/audit" {
		query("action", apiString(), "Action prefix.")
		query("user", apiString(), "Exact administrator username.")
	}
	if path == "/audit" || strings.HasPrefix(path, "/monitoring/") || path == "/nodes/{id}/web/carriers" || strings.HasSuffix(path, "/stats") && strings.HasPrefix(path, "/keys/") {
		query("from", apiSchema{"type": "string", "format": "date-time"}, "Range start in RFC3339; monitoring defaults to 24 hours ago, at most 31 days.")
		query("to", apiSchema{"type": "string", "format": "date-time"}, "Range end in RFC3339; monitoring defaults to now.")
	}
	if path == "/monitoring/overview" {
		query("step", apiSchema{"type": "integer", "minimum": 1}, "Aggregation step in seconds; default chosen from range span.")
	}
	if strings.HasSuffix(path, "/jobs") || strings.HasSuffix(path, "/diagnostics") || strings.HasSuffix(path, "/telemt-update") {
		query("limit", apiSchema{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}, "Maximum history entries.")
	}
	if strings.HasSuffix(path, "/logs") {
		query("services", apiString(), "Comma-separated systemd service names; defaults to telemt or tproxy-server for the node engine.")
		query("lines", apiSchema{"type": "integer", "minimum": 1, "maximum": 2000, "default": 200}, "Initial log lines; invalid values fall back to 200.")
		query("follow", apiSchema{"type": "string", "enum": []string{"0", "1"}}, "1 streams new lines as server-sent events.")
	}
	if strings.HasSuffix(path, "/profiles") {
		query("live", apiSchema{"type": "string", "enum": []string{"0", "1"}}, "1 requests profiles from the online node.")
	}
	if path == "/keys/{id}/qr" {
		query("node", apiSchema{"type": "string", "format": "uuid"}, "Required target node UUID.")
		params[len(params)-1].(map[string]any)["required"] = true
		query("kind", apiSchema{"type": "string", "enum": []string{"web", "tls"}, "default": "web"}, "WEB link or Fake-TLS link supported by the node.")
		query("domain", apiString(), "Optional Fake-TLS masking domain.")
	}
	if strings.HasSuffix(path, "/qr") {
		query("size", apiSchema{"type": "integer", "minimum": 128, "maximum": 1024, "default": 256}, "QR image dimensions; invalid values fall back to 256.")
	}
	if path == "/keys/{id}/subscription/qr" {
		query("short", apiSchema{"type": "string", "enum": []string{"0", "1"}}, "1 uses the short subscription slug when configured.")
	}
	if strings.HasSuffix(path, "/upload") {
		query("kind", apiSchema{"type": "string", "enum": []string{"logo", "logo_dark", "favicon", "login_bg"}}, "Asset type.")
		params[len(params)-1].(map[string]any)["required"] = true
	}
	return params
}

func openAPIResponses(route APIRoute) map[string]any {
	responses := map[string]any{}
	for code, description := range map[string]string{"400": "Malformed request.", "401": "Missing, invalid, expired or revoked API token.", "403": "Missing scope or insufficient current account role.", "404": "Resource not found.", "409": "Operation conflicts with current state or unsupported capability.", "422": "Field validation failed; error.fields contains details.", "500": "Internal server error."} {
		responses[code] = map[string]any{"description": description, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}}
	}
	status, media, schema := "200", "application/json", apiSchema{"type": "object", "additionalProperties": true}
	path := strings.TrimPrefix(route.Path, "/api/v1")
	if route.Method == "DELETE" || route.Method == "POST" && path == "/nodes/{id}/restart" {
		responses["204"] = map[string]any{"description": "Operation completed; no response body."}
		return responses
	}
	if route.Method == "POST" && (path == "/nodes" || path == "/keys" || path == "/keys/batch" || path == "/keys/import" || strings.HasPrefix(path, "/site-templates") && path != "/site-templates/validate" || path == "/branding/profiles") {
		status = "201"
	}
	if route.Method == "POST" && (path == "/nodes/{id}/apply" || path == "/nodes/{id}/web/lifecycle" || path == "/nodes/{id}/telemt-update" || path == "/fleet/updates") {
		status = "202"
	}
	switch {
	case path == "/nodes" && route.Method == "GET":
		schema = apiObject(map[string]any{"items": apiArray(openAPISchema(reflect.TypeOf(nodeJSON{}))), "total": apiSchema{"type": "integer"}})
	case path == "/nodes" && route.Method == "POST":
		schema = apiObject(map[string]any{"node": openAPISchema(reflect.TypeOf(nodeJSON{})), "install_command": apiString(), "expires_at": apiSchema{"type": "string", "format": "date-time"}})
	case path == "/nodes/{id}":
		schema = openAPISchema(reflect.TypeOf(nodeJSON{}))
	case path == "/keys":
		if route.Method == "GET" {
			schema = apiObject(map[string]any{"items": apiArray(openAPISchema(reflect.TypeOf(keyJSON{}))), "total": apiSchema{"type": "integer"}, "page": apiSchema{"type": "integer"}, "per_page": apiSchema{"type": "integer"}})
		} else {
			schema = openAPISchema(reflect.TypeOf(keyJSON{}))
		}
	case path == "/keys/{id}" || path == "/keys/{id}/bindings" || strings.HasPrefix(path, "/keys/{id}/") && (strings.HasSuffix(path, "/revoke") || strings.HasSuffix(path, "/rotate") || strings.HasSuffix(path, "/disable") || strings.HasSuffix(path, "/enable")):
		schema = openAPISchema(reflect.TypeOf(keyJSON{}))
	case path == "/nodes/{id}/apply":
		schema = apiObject(map[string]any{"queued": apiSchema{"type": "boolean"}})
	case strings.HasSuffix(path, "/web-policy"):
		schema = openAPISchema(reflect.TypeOf(webPolicyResp{}))
	case path == "/nodes/{id}/install-command":
		schema = apiObject(map[string]any{"command": apiString(), "expires_at": apiSchema{"type": "string", "format": "date-time"}})
	case path == "/nodes/{id}/registration-secret":
		schema = apiObject(map[string]any{"secret": apiString(), "address": apiString()})
	case path == "/keys/{id}/subscription" && route.Method == "POST":
		schema = apiObject(map[string]any{"url": apiString(), "qr_data_uri": apiString()})
	case strings.HasSuffix(path, "/qr"):
		media, schema = "image/png", apiSchema{"type": "string", "format": "binary"}
	case strings.HasSuffix(path, "/site/preview"):
		media, schema = "text/html", apiString()
	case strings.HasSuffix(path, "/logs"):
		media, schema = "text/event-stream", apiString()
	case path == "/nodes/{id}/metrics":
		media, schema = "text/plain", apiString()
	}
	responses[status] = map[string]any{"description": "Success. Read-only tokens omit proxy secrets and subscription URLs from user records.", "content": map[string]any{media: map[string]any{"schema": schema}}}
	return responses
}
