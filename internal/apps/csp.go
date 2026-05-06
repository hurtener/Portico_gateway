package apps

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// CSPConfig is the operator-tunable Content Security Policy applied to
// every `text/html` MCP App resource. Defaults are conservative ('self'
// only, no inline scripts); operators relax via portico.yaml or a
// per-server override.
type CSPConfig struct {
	DefaultSrc []string `yaml:"default_src" json:"default_src"`
	ScriptSrc  []string `yaml:"script_src"  json:"script_src"`
	StyleSrc   []string `yaml:"style_src"   json:"style_src"`
	ImgSrc     []string `yaml:"img_src"     json:"img_src"`
	ConnectSrc []string `yaml:"connect_src" json:"connect_src"`
	FrameSrc   []string `yaml:"frame_src"   json:"frame_src"`
	Sandbox    string   `yaml:"sandbox"     json:"sandbox"`
}

// DefaultCSP returns the project-wide default policy. Conservative:
// 'self' for everything, sandbox limited to allow-scripts.
func DefaultCSP() CSPConfig {
	return CSPConfig{
		DefaultSrc: []string{"'self'"},
		ScriptSrc:  []string{"'self'"},
		StyleSrc:   []string{"'self'"},
		ImgSrc:     []string{"'self'", "data:"},
		ConnectSrc: []string{"'self'"},
		FrameSrc:   []string{"'self'"},
		Sandbox:    "allow-scripts",
	}
}

// WithDefaults returns a CSPConfig with empty fields filled in from
// DefaultCSP. Use this in constructors so partial operator config
// remains valid.
func (c CSPConfig) WithDefaults() CSPConfig {
	d := DefaultCSP()
	if len(c.DefaultSrc) == 0 {
		c.DefaultSrc = d.DefaultSrc
	}
	if len(c.ScriptSrc) == 0 {
		c.ScriptSrc = d.ScriptSrc
	}
	if len(c.StyleSrc) == 0 {
		c.StyleSrc = d.StyleSrc
	}
	if len(c.ImgSrc) == 0 {
		c.ImgSrc = d.ImgSrc
	}
	if len(c.ConnectSrc) == 0 {
		c.ConnectSrc = d.ConnectSrc
	}
	if len(c.FrameSrc) == 0 {
		c.FrameSrc = d.FrameSrc
	}
	if c.Sandbox == "" {
		c.Sandbox = d.Sandbox
	}
	return c
}

// Header assembles the CSP header value (suitable for a
// Content-Security-Policy HTTP header or a <meta http-equiv> tag).
func (c CSPConfig) Header() string {
	var b strings.Builder
	directive := func(name string, srcs []string) {
		if len(srcs) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(strings.Join(srcs, " "))
	}
	directive("default-src", c.DefaultSrc)
	directive("script-src", c.ScriptSrc)
	directive("style-src", c.StyleSrc)
	directive("img-src", c.ImgSrc)
	directive("connect-src", c.ConnectSrc)
	directive("frame-src", c.FrameSrc)
	return b.String()
}

// Compose wraps an HTML body with CSP enforcement. Strategy:
//   - Parse the body with golang.org/x/net/html (tolerant of malformed input).
//   - Inject `<meta http-equiv="Content-Security-Policy" content="...">` as
//     the first child of <head>. If <head> is missing, the parser creates
//     one as part of normalising the document.
//   - Return the rendered HTML plus a `meta` map intended for inclusion in
//     the surrounding ResourceContent's `_meta.portico` block (csp +
//     sandbox).
//
// Compose is best-effort: if parsing fails the original body is returned
// untouched and the meta map is still emitted so the host can apply
// out-of-band protections.
func (c CSPConfig) Compose(body []byte) ([]byte, map[string]string) {
	meta := map[string]string{
		"csp":     c.Header(),
		"sandbox": c.Sandbox,
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return body, meta
	}
	headNode := findOrCreateHead(doc)
	if headNode == nil {
		return body, meta
	}
	metaNode := &html.Node{
		Type: html.ElementNode,
		Data: "meta",
		Attr: []html.Attribute{
			{Key: "http-equiv", Val: "Content-Security-Policy"},
			{Key: "content", Val: c.Header()},
		},
	}
	if headNode.FirstChild != nil {
		headNode.InsertBefore(metaNode, headNode.FirstChild)
	} else {
		headNode.AppendChild(metaNode)
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return body, meta
	}
	return buf.Bytes(), meta
}

// findOrCreateHead returns the <head> element of doc, creating one
// inside <html> if it doesn't exist. Returns nil only if the parser
// produced a tree without an <html> root, which shouldn't happen in
// practice but is handled defensively.
func findOrCreateHead(n *html.Node) *html.Node {
	var htmlNode *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "html" {
			htmlNode = c
			break
		}
	}
	if htmlNode == nil {
		// Walk one more level (some inputs are wrapped in DocumentNode).
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.DocumentNode {
				return findOrCreateHead(c)
			}
		}
		return nil
	}
	for c := htmlNode.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "head" {
			return c
		}
	}
	head := &html.Node{Type: html.ElementNode, Data: "head"}
	if htmlNode.FirstChild != nil {
		htmlNode.InsertBefore(head, htmlNode.FirstChild)
	} else {
		htmlNode.AppendChild(head)
	}
	return head
}
