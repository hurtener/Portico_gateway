package protocol

// AggregateServerCaps unions a set of downstream-server capabilities into the
// effective capability advertised by Portico. A capability is present iff at
// least one downstream advertises it. Portico-internal additions (always-on
// like the gateway's own logging) are layered after this aggregation.
func AggregateServerCaps(downstream []ServerCapabilities) ServerCapabilities {
	out := ServerCapabilities{}
	for _, c := range downstream {
		if c.Tools != nil {
			if out.Tools == nil {
				out.Tools = &ToolsCapability{}
			}
			out.Tools.ListChanged = out.Tools.ListChanged || c.Tools.ListChanged
		}
		if c.Resources != nil {
			if out.Resources == nil {
				out.Resources = &ResourcesCapability{}
			}
			out.Resources.Subscribe = out.Resources.Subscribe || c.Resources.Subscribe
			out.Resources.ListChanged = out.Resources.ListChanged || c.Resources.ListChanged
		}
		if c.Prompts != nil {
			if out.Prompts == nil {
				out.Prompts = &PromptsCapability{}
			}
			out.Prompts.ListChanged = out.Prompts.ListChanged || c.Prompts.ListChanged
		}
		if c.Logging != nil && out.Logging == nil {
			out.Logging = &LoggingCapability{}
		}
	}
	return out
}

// ClientCapsRecord captures the fields Portico cares about from a client's
// declared capabilities; used by Phase 5 (approval flow) to decide whether to
// elicit or fall back to a structured error.
type ClientCapsRecord struct {
	HasElicitation bool
	HasSampling    bool
	HasRoots       bool
}

// RecordClientCaps extracts a ClientCapsRecord from a full ClientCapabilities.
func RecordClientCaps(c ClientCapabilities) ClientCapsRecord {
	return ClientCapsRecord{
		HasElicitation: c.Elicitation != nil,
		HasSampling:    c.Sampling != nil,
		HasRoots:       c.Roots != nil,
	}
}
