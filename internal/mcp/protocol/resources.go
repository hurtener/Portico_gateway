package protocol

import "encoding/json"

// Resource is the on-the-wire shape returned by `resources/list`. Portico
// treats every field as opaque except URI and MimeType (used for routing
// and CSP wrapping).
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Annotations *Annotations    `json:"annotations,omitempty"`
	Size        *int64          `json:"size,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// ResourceTemplate describes a parameterised resource URI.
type ResourceTemplate struct {
	URITemplate string       `json:"uriTemplate"`
	Name        string       `json:"name,omitempty"`
	Description string       `json:"description,omitempty"`
	MimeType    string       `json:"mimeType,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

// Annotations is the MCP `annotations` block (audience + priority hints
// that callers may use for ranking). Portico treats it as opaque payload.
type Annotations struct {
	Audience []string `json:"audience,omitempty"`
	Priority *float64 `json:"priority,omitempty"`
}

// ListResourcesParams is the params for `resources/list`.
type ListResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourcesResult is the result of `resources/list`.
type ListResourcesResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// ListResourceTemplatesParams is the params for `resources/templates/list`.
type ListResourceTemplatesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourceTemplatesResult is the result of `resources/templates/list`.
type ListResourceTemplatesResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	NextCursor        string             `json:"nextCursor,omitempty"`
}

// ReadResourceParams is the params for `resources/read`.
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult is the result of `resources/read`.
type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent is one chunk of a `resources/read` response. Either
// Text or Blob is populated; Blob is base64-encoded.
type ResourceContent struct {
	URI      string          `json:"uri"`
	MimeType string          `json:"mimeType,omitempty"`
	Text     string          `json:"text,omitempty"`
	Blob     string          `json:"blob,omitempty"`
	Meta     json.RawMessage `json:"_meta,omitempty"`
}

// SubscribeResourceParams is the params for `resources/subscribe`.
type SubscribeResourceParams struct {
	URI string `json:"uri"`
}

// UnsubscribeResourceParams is the params for `resources/unsubscribe`.
type UnsubscribeResourceParams struct {
	URI string `json:"uri"`
}
