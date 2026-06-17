package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// BindingLevel controls the granularity of the projected virtual file system.
type BindingLevel string

const (
	// BindingServer renders one stub file per server (the default): every tool a
	// server exposes is a function in servers/<server>.pyi.
	BindingServer BindingLevel = "server"
	// BindingTool additionally renders one stub file per tool under
	// servers/<server>/<tool>.pyi, for clients that want to load a single tool's
	// signature without the whole server.
	BindingTool BindingLevel = "tool"
)

// ToolRef binds one projected Starlark callable to its dispatcher-namespaced
// tool name. Module/Func are the Python identifiers the model writes
// (module.func(...)); Namespaced is what the dispatcher resolves. The runtime's
// ToolBinding is constructed from these by the codemode wiring layer.
type ToolRef struct {
	Module     string
	Func       string
	Namespaced string
}

// Projection is the deterministic virtual-FS + binding set for one snapshot at
// one binding level. Files maps a virtual path to its content; Tools is the
// list of callable bindings the sandbox should expose. Given the same snapshot
// and level, Project produces byte-identical Files (acceptance #2/#3).
type Projection struct {
	Files map[string]string
	Tools []ToolRef
}

// Project renders a snapshot into its virtual file system and binding set. It is
// a pure function: no I/O, no clock, deterministic ordering throughout.
func Project(snap *snapshots.Snapshot, level BindingLevel) Projection {
	proj := Projection{Files: map[string]string{}}
	if snap == nil {
		proj.Files["index.md"] = renderIndex(nil)
		return proj
	}

	byServer := groupToolsByServer(snap.Tools)
	servers := make([]string, 0, len(byServer))
	for s := range byServer {
		servers = append(servers, s)
	}
	sort.Strings(servers)

	for _, serverID := range servers {
		tools := byServer[serverID]
		module := sanitizePyName(serverID)
		proj.Files["servers/"+serverID+".pyi"] = renderServerStub(serverID, module, tools)

		for _, ti := range tools {
			_, toolName, ok := namespace.SplitTool(ti.NamespacedName)
			if !ok {
				continue
			}
			proj.Tools = append(proj.Tools, ToolRef{
				Module:     module,
				Func:       sanitizePyName(toolName),
				Namespaced: ti.NamespacedName,
			})
			if level == BindingTool {
				path := "servers/" + serverID + "/" + toolName + ".pyi"
				proj.Files[path] = renderToolStub(module, ti)
			}
		}
	}

	proj.Files["index.md"] = renderIndex(snapshotServerSummary(servers, byServer))
	return proj
}

// groupToolsByServer buckets tools by server id, each bucket sorted by
// namespaced name for determinism.
func groupToolsByServer(tools []snapshots.ToolInfo) map[string][]snapshots.ToolInfo {
	byServer := map[string][]snapshots.ToolInfo{}
	for _, ti := range tools {
		byServer[ti.ServerID] = append(byServer[ti.ServerID], ti)
	}
	for s := range byServer {
		sort.Slice(byServer[s], func(i, j int) bool {
			return byServer[s][i].NamespacedName < byServer[s][j].NamespacedName
		})
	}
	return byServer
}

// renderServerStub renders servers/<server>.pyi: a header naming the Python
// module, then one function signature per tool.
func renderServerStub(serverID, module string, tools []snapshots.ToolInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Server %q exposed as Starlark module %q\n", serverID, module)
	fmt.Fprintf(&b, "# %d tool(s). Call as: %s.<function>(...)\n\n", len(tools), module)
	for i, ti := range tools {
		_, toolName, ok := namespace.SplitTool(ti.NamespacedName)
		if !ok {
			toolName = ti.NamespacedName
		}
		b.WriteString(ToolStub(toolName, ti.Description, ti.InputSchema))
		if i < len(tools)-1 {
			b.WriteString("\n\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

// renderToolStub renders a single-tool .pyi (BindingTool level).
func renderToolStub(module string, ti snapshots.ToolInfo) string {
	_, toolName, ok := namespace.SplitTool(ti.NamespacedName)
	if !ok {
		toolName = ti.NamespacedName
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s.%s\n", module, sanitizePyName(toolName))
	b.WriteString(ToolStub(toolName, ti.Description, ti.InputSchema))
	b.WriteString("\n")
	return b.String()
}

// serverSummary is one line of the index.
type serverSummary struct {
	id    string
	count int
}

func snapshotServerSummary(servers []string, byServer map[string][]snapshots.ToolInfo) []serverSummary {
	out := make([]serverSummary, 0, len(servers))
	for _, s := range servers {
		out = append(out, serverSummary{id: s, count: len(byServer[s])})
	}
	return out
}

// renderIndex renders index.md: a short orientation file listing the servers
// and their tool counts.
func renderIndex(servers []serverSummary) string {
	var b strings.Builder
	b.WriteString("# Code Mode tool catalog\n\n")
	b.WriteString("Each server is a Starlark module. Read `servers/<server>.pyi` for its\n")
	b.WriteString("function signatures, then call tools from `mcp.executeToolCode`.\n\n")
	if len(servers) == 0 {
		b.WriteString("_No servers are visible in this snapshot._\n")
		return b.String()
	}
	b.WriteString("## Servers\n\n")
	for _, s := range servers {
		fmt.Fprintf(&b, "- `servers/%s.pyi` — %d tool(s)\n", s.id, s.count)
	}
	return b.String()
}
