package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

// yamlUnmarshal is a thin wrapper around gopkg.in/yaml.v3 that lets
// the rest of the file talk in terms of an internal helper.
func yamlUnmarshal(body []byte, v any) error { return yaml.Unmarshal(body, v) }

// SkillProvider implements the resource + prompt surface for the
// gateway: it materializes synthetic `skill://` resources and named
// prompts from the catalog, applies enablement filtering, and reads
// back through the source on demand.
type SkillProvider struct {
	catalog    *Catalog
	enablement *Enablement
	indexGen   *IndexGenerator
}

// NewSkillProvider wires the catalog + enablement. indexGen is
// optional; nil disables the synthetic skill://_index resource.
func NewSkillProvider(catalog *Catalog, enablement *Enablement, indexGen *IndexGenerator) *SkillProvider {
	return &SkillProvider{
		catalog:    catalog,
		enablement: enablement,
		indexGen:   indexGen,
	}
}

// ListResources returns every synthetic skill:// resource visible to
// the (tenant, session, plan) combination. The returned slice is
// already namespaced; callers concatenate it with downstream-server
// resources.
func (p *SkillProvider) ListResources(ctx context.Context, tenantID, sessionID, plan string, ents []string) ([]protocol.Resource, error) {
	if p == nil || p.catalog == nil {
		return nil, nil
	}
	out := make([]protocol.Resource, 0)
	// _index is always present; clients use it to discover the rest.
	if p.indexGen != nil {
		out = append(out, protocol.Resource{
			URI:         "skill://_index",
			Name:        "Skill index",
			Description: "Per-tenant skill catalog with enablement + missing-tools status.",
			MimeType:    "application/json",
			Meta:        skillMetaJSON(map[string]string{"synthetic": "true"}),
		})
	}
	for _, s := range p.catalog.ForTenant(ents, plan) {
		on, err := p.enablement.IsEnabled(ctx, tenantID, sessionID, s.Manifest.ID)
		if err != nil {
			return nil, err
		}
		if !on {
			continue
		}
		out = append(out, p.synthesizeResources(ctx, s)...)
	}
	return out, nil
}

// synthesizeResources generates the manifest, instructions, and
// declared resource/prompt files as protocol.Resource entries. Bytes
// are NOT loaded here — that happens lazily in ReadResource.
func (p *SkillProvider) synthesizeResources(ctx context.Context, s *Skill) []protocol.Resource {
	if s == nil || s.Manifest == nil {
		return nil
	}
	ns := s.Namespace()
	name := s.Name()
	base := "skill://" + ns + "/" + name + "/"
	out := make([]protocol.Resource, 0, 2+len(s.Manifest.Resources)+len(s.Manifest.Prompts))

	// manifest.yaml
	out = append(out, protocol.Resource{
		URI:         base + "manifest.yaml",
		Name:        "manifest.yaml",
		Description: s.Manifest.Description,
		MimeType:    "application/yaml",
		Meta:        skillMetaJSON(map[string]string{"synthetic": "true", "skillID": s.Manifest.ID}),
	})
	// SKILL.md (instructions)
	if s.Manifest.Instructions != "" {
		out = append(out, protocol.Resource{
			URI:         base + s.Manifest.Instructions,
			Name:        s.Manifest.Title,
			Description: s.Manifest.Description,
			MimeType:    "text/markdown",
			Meta:        skillMetaJSON(map[string]string{"synthetic": "true", "skillID": s.Manifest.ID}),
		})
	}
	for _, r := range s.Manifest.Resources {
		out = append(out, protocol.Resource{
			URI:      base + r,
			Name:     filepath.Base(r),
			MimeType: mimeForExt(r),
			Meta:     skillMetaJSON(map[string]string{"synthetic": "true", "skillID": s.Manifest.ID}),
		})
	}
	for _, r := range s.Manifest.Prompts {
		out = append(out, protocol.Resource{
			URI:      base + r,
			Name:     filepath.Base(r),
			MimeType: mimeForExt(r),
			Meta:     skillMetaJSON(map[string]string{"synthetic": "true", "skillID": s.Manifest.ID, "kind": "prompt-source"}),
		})
	}
	return out
}

// ReadResource resolves a skill:// URI to its content. The URI scheme
// must be skill:// (callers should branch on that).
func (p *SkillProvider) ReadResource(ctx context.Context, tenantID, sessionID, uri string) (*protocol.ReadResourceResult, error) {
	if p == nil || p.catalog == nil {
		return nil, fmt.Errorf("skill provider not configured")
	}
	if uri == "skill://_index" {
		if p.indexGen == nil {
			return nil, fmt.Errorf("index generator not configured")
		}
		body, err := p.indexGen.Render(ctx, tenantID, sessionID)
		if err != nil {
			return nil, err
		}
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "application/json", Text: string(body)},
		}}, nil
	}
	ns, name, rel, ok := splitSkillURI(uri)
	if !ok {
		return nil, fmt.Errorf("malformed skill uri: %q", uri)
	}
	skillID := ns + "." + name
	skill, ok := p.catalog.Get(skillID)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", skillID)
	}
	// manifest.yaml is rendered from the in-memory Manifest so the bytes
	// reflect the parsed (canonical) view — handy when SKILL.md and
	// manifest are edited together and we want consistency.
	if rel == "manifest.yaml" {
		body, err := renderManifestYAML(skill.Manifest)
		if err != nil {
			return nil, err
		}
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "application/yaml", Text: string(body)},
		}}, nil
	}
	rc, info, err := skill.Source.ReadFile(ctx, skill.Ref, rel)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	mime := info.MIMEType
	if mime == "" {
		mime = mimeForExt(rel)
	}
	return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
		{URI: uri, MimeType: mime, Text: string(body)},
	}}, nil
}

// ListPrompts returns the namespaced prompt list across enabled
// skills. Each prompt is registered as `{skill.id}.{filename-no-ext}`.
func (p *SkillProvider) ListPrompts(ctx context.Context, tenantID, sessionID, plan string, ents []string) ([]protocol.Prompt, error) {
	if p == nil || p.catalog == nil {
		return nil, nil
	}
	out := make([]protocol.Prompt, 0)
	for _, s := range p.catalog.ForTenant(ents, plan) {
		on, err := p.enablement.IsEnabled(ctx, tenantID, sessionID, s.Manifest.ID)
		if err != nil {
			return nil, err
		}
		if !on {
			continue
		}
		for _, rel := range s.Manifest.Prompts {
			meta := readPromptFrontmatter(ctx, s, rel)
			name := s.Manifest.ID + "." + stripExt(filepath.Base(rel))
			if meta.Name != "" {
				name = s.Manifest.ID + "." + meta.Name
			}
			out = append(out, protocol.Prompt{
				Name:        name,
				Description: meta.Description,
				Arguments:   meta.Arguments,
			})
		}
	}
	return out, nil
}

// GetPrompt renders the named prompt with the supplied arguments. Name
// must be of the form `{skill.id}.{prompt-name}`.
func (p *SkillProvider) GetPrompt(ctx context.Context, tenantID, sessionID, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	if p == nil || p.catalog == nil {
		return nil, fmt.Errorf("skill provider not configured")
	}
	skillID, rest, ok := splitSkillPrompt(name)
	if !ok {
		return nil, fmt.Errorf("prompt name must be `{skill.id}.{name}`: %q", name)
	}
	skill, ok := p.catalog.Get(skillID)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", skillID)
	}
	on, err := p.enablement.IsEnabled(ctx, tenantID, sessionID, skill.Manifest.ID)
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, fmt.Errorf("skill not enabled for this session: %s", skill.Manifest.ID)
	}
	// Find the prompt file.
	var match string
	for _, rel := range skill.Manifest.Prompts {
		fm := readPromptFrontmatter(ctx, skill, rel)
		base := stripExt(filepath.Base(rel))
		if fm.Name == rest || base == rest {
			match = rel
			break
		}
	}
	if match == "" {
		return nil, fmt.Errorf("prompt %q not declared by skill %s", rest, skillID)
	}
	rc, _, err := skill.Source.ReadFile(ctx, skill.Ref, match)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	rendered, fm, err := renderPromptBody(body, args)
	if err != nil {
		return nil, err
	}
	return &protocol.GetPromptResult{
		Description: fm.Description,
		Messages: []protocol.PromptMessage{
			{
				Role: "user",
				Content: protocol.ContentBlock{
					Type: "text",
					Text: rendered,
				},
			},
		},
	}, nil
}

// ----- helpers -----------------------------------------------------------

// splitSkillURI parses skill://<ns>/<name>/<rel> into its three parts.
func splitSkillURI(uri string) (ns, name, rel string, ok bool) {
	const prefix = "skill://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", "", false
	}
	rest := uri[len(prefix):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 {
		return "", "", "", false
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// splitSkillPrompt parses `{skill.id}.{name}` into (skillID, name).
// skill IDs themselves contain a single dot (`namespace.name`); the
// prompt suffix lives after the SECOND dot.
func splitSkillPrompt(qualified string) (skillID, prompt string, ok bool) {
	first := strings.IndexByte(qualified, '.')
	if first <= 0 || first == len(qualified)-1 {
		return "", "", false
	}
	rest := qualified[first+1:]
	second := strings.IndexByte(rest, '.')
	if second <= 0 || second == len(rest)-1 {
		return "", "", false
	}
	return qualified[:first+1+second], rest[second+1:], true
}

func stripExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i]
	}
	return name
}

func mimeForExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	}
	return "application/octet-stream"
}

func skillMetaJSON(attrs map[string]string) json.RawMessage {
	root := map[string]any{
		"portico": attrs,
	}
	body, _ := json.Marshal(root)
	return body
}

func renderManifestYAML(m *manifest.Manifest) ([]byte, error) {
	// We re-emit JSON here; YAML clients accept either since YAML is a
	// JSON superset. Importing a YAML encoder library purely to round-
	// trip a manifest is overkill.
	return json.MarshalIndent(m, "", "  ")
}

// PromptFrontmatter is the `---`-fenced YAML block that prompt files
// may carry. All fields are optional.
type PromptFrontmatter struct {
	Name        string                    `yaml:"name" json:"name,omitempty"`
	Description string                    `yaml:"description" json:"description,omitempty"`
	Arguments   []protocol.PromptArgument `yaml:"arguments" json:"arguments,omitempty"`
}

// readPromptFrontmatter reads the prompt file and parses its
// frontmatter block. Errors are silently absorbed (returns zero
// frontmatter) so a missing file or malformed frontmatter does not
// take down `prompts/list`.
func readPromptFrontmatter(ctx context.Context, s *Skill, rel string) PromptFrontmatter {
	rc, _, err := s.Source.ReadFile(ctx, s.Ref, rel)
	if err != nil {
		return PromptFrontmatter{}
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return PromptFrontmatter{}
	}
	fm, _, _ := splitFrontmatter(body)
	return fm
}

// splitFrontmatter returns the parsed frontmatter and the rest of the
// document (after the closing `---`). When the file lacks a
// frontmatter block, returns zero PromptFrontmatter and the full body.
func splitFrontmatter(body []byte) (PromptFrontmatter, []byte, error) {
	const sep = "---"
	if !bytes.HasPrefix(body, []byte(sep)) {
		return PromptFrontmatter{}, body, nil
	}
	rest := body[len(sep):]
	// Skip an optional newline after the opening sep.
	rest = bytes.TrimLeft(rest, "\n")
	end := bytes.Index(rest, []byte("\n"+sep))
	if end < 0 {
		return PromptFrontmatter{}, body, nil
	}
	yamlBlock := rest[:end]
	doc := bytes.TrimLeft(rest[end+len(sep)+1:], "\n")
	var fm PromptFrontmatter
	if err := yamlUnmarshal(yamlBlock, &fm); err != nil {
		return PromptFrontmatter{}, doc, err
	}
	return fm, doc, nil
}

// renderPromptBody parses the optional frontmatter and runs Go's
// text/template against the document body using args. Templates are
// scoped to leaf substitution (`{{owner}}`); no `{{template}}` or
// `{{include}}` directives — we explicitly forbid them.
func renderPromptBody(body []byte, args map[string]string) (string, PromptFrontmatter, error) {
	fm, doc, err := splitFrontmatter(body)
	if err != nil {
		// Frontmatter parse failure is non-fatal: render the raw body.
		fm = PromptFrontmatter{}
	}
	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(string(doc))
	if err != nil {
		return string(doc), fm, fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, args); err != nil {
		return string(doc), fm, fmt.Errorf("template render: %w", err)
	}
	return buf.String(), fm, nil
}
