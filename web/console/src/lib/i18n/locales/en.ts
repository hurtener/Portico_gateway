/**
 * English locale — the canonical source. Every other locale must mirror
 * the keys here. The `i18n-check` npm script enforces parity.
 *
 * Keys follow `<area>.<purpose>` and prefer flat over deeply nested
 * (search-friendly).
 */
export default {
  // Brand
  'brand.name': 'Portico',
  'brand.tagline': 'A governed gateway for MCP servers',

  // Common UI
  'common.refresh': 'Refresh',
  'common.search': 'Search',
  'common.save': 'Save',
  'common.cancel': 'Cancel',
  'common.delete': 'Delete',
  'common.enable': 'Enable',
  'common.disable': 'Disable',
  'common.close': 'Close',
  'common.loading': 'Loading…',
  'common.empty': 'No items.',
  'common.yes': 'yes',
  'common.no': 'no',
  'common.copy': 'Copy',
  'common.copied': 'Copied',
  'common.expand': 'Expand',
  'common.collapse': 'Collapse',
  'common.loadMore': 'Load more',
  'common.viewAll': 'View all',
  'common.dash': '—',

  // Navigation groups + items
  'nav.overview': 'Overview',
  'nav.section.catalog': 'Catalog',
  'nav.section.operations': 'Operations',
  'nav.section.admin': 'Admin',
  'nav.servers': 'Servers',
  'nav.resources': 'Resources',
  'nav.prompts': 'Prompts',
  'nav.apps': 'Apps',
  'nav.skills': 'Skills',
  'nav.sessions': 'Sessions',
  'nav.approvals': 'Approvals',
  'nav.audit': 'Audit',
  'nav.snapshots': 'Snapshots',
  'nav.secrets': 'Secrets',

  // TopBar / chrome
  'topbar.search': 'Search… (⌘K)',
  'topbar.notifications': 'Notifications',
  'topbar.notifications.empty': 'No new notifications',
  'topbar.notifications.markRead': 'Mark all read',
  'topbar.profile.role.admin': 'Admin (local)',
  'topbar.profile.signOut': 'Sign out',
  'topbar.profile.signOut.disabled': 'Authentication arrives in Phase 12.',
  'topbar.theme.light': 'Light',
  'topbar.theme.dark': 'Dark',
  'topbar.theme.system': 'System',
  'topbar.locale.en': 'EN',
  'topbar.locale.es': 'ES',
  'topbar.envBadge': 'Local · dev',

  // Sidebar status
  'sidebar.health': 'Health',
  'sidebar.ready': 'Ready',
  'sidebar.collapse': 'Collapse sidebar',
  'sidebar.expand': 'Expand sidebar',

  // Landing page
  'landing.title': 'Portico Console',
  'landing.lede':
    'Multi-tenant gateway and Skill runtime for the Model Context Protocol. The Console is the operator surface for managing servers, authoring skills, and inspecting sessions.',
  'landing.tile.health': 'Health',
  'landing.tile.ready': 'Ready',
  'landing.tile.sessions': 'Active sessions',
  'landing.tile.approvals': 'Pending approvals',
  'landing.tile.lastSnapshot': 'Last snapshot',
  'landing.tile.drift24h': 'Drift events (24h)',
  'landing.section.recentSessions': 'Recent sessions',
  'landing.section.recentApprovals': 'Recent approvals',
  'landing.section.recentAudit': 'Recent audit events',
  'landing.empty.sessions': 'No sessions yet.',
  'landing.empty.approvals': 'No pending approvals.',
  'landing.empty.audit': 'No recent events.',
  'landing.status.ok': 'ok',
  'landing.status.down': 'down',
  'landing.status.pending': 'pending',
  'landing.relTime.never': 'never',
  'landing.relTime.justNow': 'just now',
  'landing.relTime.minutes': '{n}m ago',
  'landing.relTime.hours': '{n}h ago',
  'landing.relTime.days': '{n}d ago',

  // Servers
  'servers.title': 'Servers',
  'servers.description':
    'Downstream MCP servers registered for this tenant. Toggle enables, drill into details, or refresh the live status.',
  'servers.col.id': 'ID',
  'servers.col.displayName': 'Display name',
  'servers.col.transport': 'Transport',
  'servers.col.mode': 'Mode',
  'servers.col.status': 'Status',
  'servers.col.enabled': 'Enabled',
  'servers.empty.title': 'No servers registered',
  'servers.empty.description':
    'Once an operator registers downstream MCP servers, they appear here.',

  // Server detail
  'serverDetail.spec': 'Spec',
  'serverDetail.instances': 'Instances ({count})',
  'serverDetail.instances.empty.title': 'No active instances',
  'serverDetail.instances.empty.description':
    'The supervisor will spawn an instance when a session needs this server.',
  'serverDetail.notFound.title': 'Server not found',
  'serverDetail.notFound.description': 'No server with id {id}.',
  'serverDetail.action.reload': 'Drain & reload',
  'serverDetail.field.transport': 'Transport',
  'serverDetail.field.mode': 'Runtime mode',
  'serverDetail.field.status': 'Status',
  'serverDetail.field.enabled': 'Enabled',
  'serverDetail.field.command': 'Command',
  'serverDetail.field.args': 'Args',
  'serverDetail.field.url': 'URL',

  // Resources
  'resources.title': 'Resources',
  'resources.description': 'Every resource the gateway aggregates across downstream MCP servers.',
  'resources.col.server': 'Server',
  'resources.col.uri': 'URI',
  'resources.col.name': 'Name',
  'resources.col.mime': 'MIME',
  'resources.empty.title': 'No resources discovered',
  'resources.empty.description':
    'Once a downstream server registers resource templates, they appear here.',

  // Prompts
  'prompts.title': 'Prompts',
  'prompts.description':
    'Server-defined prompts available to clients. Required arguments are highlighted.',
  'prompts.col.server': 'Server',
  'prompts.col.name': 'Name',
  'prompts.col.description': 'Description',
  'prompts.col.arguments': 'Arguments',
  'prompts.empty.title': 'No prompts available',
  'prompts.empty.description': 'When servers expose prompts, they appear here.',

  // Apps
  'apps.title': 'MCP Apps',
  'apps.description':
    'Every ui:// resource discovered across downstream servers. Phase 5 adds policy filtering; preview rendering arrives later in dev mode.',
  'apps.empty.title': 'No MCP App resources discovered yet',
  'apps.empty.description':
    'Servers exposing ui:// resources will populate this view automatically.',
  'apps.field.server': 'Server',
  'apps.field.uri': 'URI',
  'apps.field.upstream': 'Upstream',

  // Skills
  'skills.title': 'Skills',
  'skills.description': 'Loaded Skill Packs and their per-tenant enablement state.',
  'skills.col.id': 'ID',
  'skills.col.title': 'Title',
  'skills.col.version': 'Version',
  'skills.col.status': 'Status',
  'skills.col.missing': 'Missing tools',
  'skills.status.enabled': 'enabled',
  'skills.status.disabled': 'disabled',
  'skills.status.missingTools': 'missing tools',
  'skills.empty.title': 'No skills loaded',
  'skills.empty.description':
    'Configure skills.sources in portico.yaml or install skills via the upcoming Phase 8 sources page.',

  // Skill detail
  'skillDetail.action.enableForTenant': 'Enable for tenant',
  'skillDetail.action.disableForTenant': 'Disable for tenant',
  'skillDetail.warnings': 'Warnings',
  'skillDetail.manifest': 'Manifest',
  'skillDetail.notFound': 'Skill not found.',

  // Sessions
  'sessions.title': 'Sessions',
  'sessions.description':
    'Active gateway sessions, the tools they invoked, and their replay timelines.',
  'sessions.empty.title': 'Session telemetry coming online',
  'sessions.empty.description':
    'Phase 6 wires the live session feed. The replay timeline ships with the inspector route in Phase 11.',

  // Approvals
  'approvals.title': 'Pending approvals',
  'approvals.description':
    'Tool calls and elicitations awaiting an operator decision. Polled every 2 seconds.',
  'approvals.col.tool': 'Tool',
  'approvals.col.risk': 'Risk',
  'approvals.col.session': 'Session',
  'approvals.col.created': 'Created',
  'approvals.col.expires': 'Expires',
  'approvals.action.approve': 'Approve',
  'approvals.action.deny': 'Deny',
  'approvals.empty.title': 'No pending approvals',
  'approvals.empty.description': 'When tools require operator review, they appear here.',

  // Audit
  'audit.title': 'Audit log',
  'audit.description':
    'Tenant-scoped, redacted audit events. Filter by type slug; payloads are JSON-encoded.',
  'audit.col.when': 'When',
  'audit.col.type': 'Type',
  'audit.col.tenant': 'Tenant',
  'audit.col.session': 'Session',
  'audit.col.payload': 'Payload',
  'audit.filter.placeholder': 'event type (e.g. tool_call.complete)',
  'audit.empty.title': 'No events match',
  'audit.empty.description':
    'Try a broader filter, or run a session through the gateway to populate the log.',

  // Snapshots
  'snapshots.title': 'Catalog snapshots',
  'snapshots.description': 'Per-session catalog snapshots — used for replay and drift detection.',
  'snapshots.col.id': 'ID',
  'snapshots.col.tenant': 'Tenant',
  'snapshots.col.session': 'Session',
  'snapshots.col.tools': 'Tools',
  'snapshots.col.created': 'Created',
  'snapshots.col.hash': 'Hash',
  'snapshots.empty.title': 'No snapshots yet',
  'snapshots.empty.description':
    'Create a session against the gateway to materialize a catalog snapshot.',

  // Snapshot detail
  'snapshotDetail.title': 'Snapshot {id}',
  'snapshotDetail.field.tenant': 'Tenant',
  'snapshotDetail.field.session': 'Session',
  'snapshotDetail.field.created': 'Created',
  'snapshotDetail.field.hash': 'Overall hash',
  'snapshotDetail.warnings': 'Warnings',
  'snapshotDetail.servers': 'Servers ({count})',
  'snapshotDetail.tools': 'Tools ({count})',
  'snapshotDetail.skills': 'Skills ({count})',
  'snapshotDetail.notFound.title': 'Snapshot not found',
  'snapshotDetail.notFound.description': 'No snapshot with id {id}.',

  // Snapshot diff
  'snapshotDiff.title': 'Snapshot diff',
  'snapshotDiff.description': 'Comparing {a} → {b}',
  'snapshotDiff.empty.title': 'No changes',
  'snapshotDiff.empty.description': 'Both snapshots resolve to the same canonical fingerprint.',
  'snapshotDiff.section.tools': 'Tools',
  'snapshotDiff.section.resources': 'Resources',
  'snapshotDiff.section.prompts': 'Prompts',
  'snapshotDiff.section.skills': 'Skills',
  'snapshotDiff.added': 'Added',
  'snapshotDiff.removed': 'Removed',
  'snapshotDiff.modified': 'Modified',
  'snapshotDiff.fieldsChanged': 'fields changed: {fields}',
  'snapshotDiff.noChanges.tools': 'No tool changes.',
  'snapshotDiff.noChanges.resources': 'No resource changes.',
  'snapshotDiff.noChanges.prompts': 'No prompt changes.',
  'snapshotDiff.noChanges.skills': 'No skill changes.',

  // Secrets (admin)
  'secrets.title': 'Vault secrets (admin)',
  'secrets.description':
    'AES-256-GCM-encrypted secret references, keyed by (tenant, name). Values are never displayed; only references appear in this list.',
  'secrets.form.title': 'Add secret',
  'secrets.form.tenant': 'Tenant',
  'secrets.form.tenant.placeholder': 'acme',
  'secrets.form.name': 'Name',
  'secrets.form.name.placeholder': 'github_token',
  'secrets.form.value': 'Value',
  'secrets.form.value.placeholder': '•••••••••',
  'secrets.form.required': 'tenant, name, and value are all required',
  'secrets.col.tenant': 'Tenant',
  'secrets.col.name': 'Name',
  'secrets.section.existing': 'Existing secrets',
  'secrets.toast.saved.title': 'Secret saved',
  'secrets.toast.saved.description': '{tenant}/{name} encrypted and stored.',
  'secrets.toast.saveFailed.title': 'Save failed',
  'secrets.toast.deleted.title': 'Secret deleted',
  'secrets.toast.deleted.description': '{tenant}/{name} purged from the vault.',
  'secrets.toast.deleteFailed.title': 'Delete failed',
  'secrets.confirmDelete': 'Delete {tenant}/{name}? This cannot be undone.',
  'secrets.empty.title': 'No secrets stored',
  'secrets.empty.description':
    "Add a credential reference above to bind it to a server's auth strategy.",
  'secrets.unavailable.title': 'Vault not configured',
  'secrets.unavailable.description':
    'Set PORTICO_VAULT_KEY before starting Portico to enable secret management. The vault stays disabled in dev mode without a key.',

  // Skill sources (Phase 8)
  'sources.title': 'Skill sources',
  'sources.description':
    'External Git/HTTP feeds plus the in-Portico authored store. Add or remove sources without restarting the gateway.',
  'sources.col.name': 'Name',
  'sources.col.driver': 'Driver',
  'sources.col.priority': 'Priority',
  'sources.col.enabled': 'Enabled',
  'sources.col.lastRefresh': 'Last refresh',
  'sources.col.status': 'Status',
  'sources.action.add': 'Add source',
  'sources.action.refresh': 'Refresh now',
  'sources.empty.title': 'No sources configured',
  'sources.empty.description':
    'Add a Git or HTTP feed, or compose authored skills directly in the Console.',
  'sources.form.name': 'Name',
  'sources.form.driver': 'Driver',
  'sources.form.config.url': 'URL',
  'sources.form.config.branch': 'Branch (optional)',
  'sources.form.config.feedUrl': 'Feed URL',
  'sources.form.config.subdir': 'Subdir glob (optional)',
  'sources.form.credentialRef': 'Vault credential (optional)',
  'sources.form.priority': 'Priority',
  'sources.form.refresh': 'Refresh seconds',
  'sources.form.required': 'Name and driver are required',
  'sources.toast.saved.title': 'Source saved',
  'sources.toast.deleted.title': 'Source removed',
  'sources.detail.lastError': 'Last error',
  'sources.detail.lastRefresh': 'Last refresh',
  'sources.detail.packs': 'Packs from this source',
  'sources.detail.notFound.title': 'Source not found',

  // Authored skills (Phase 8)
  'authored.title': 'Authored skills',
  'authored.description':
    'In-Portico Skill Packs. Compose a manifest, draft files, and publish — no rebuild required.',
  'authored.col.skillId': 'Skill ID',
  'authored.col.version': 'Version',
  'authored.col.status': 'Status',
  'authored.col.checksum': 'Checksum',
  'authored.col.created': 'Created',
  'authored.action.new': 'New skill',
  'authored.action.publish': 'Publish',
  'authored.action.archive': 'Archive',
  'authored.action.deleteDraft': 'Delete draft',
  'authored.empty.title': 'No authored skills yet',
  'authored.empty.description':
    'Create a draft from the manifest editor, then publish it for the active tenant.',
  'authored.editor.manifest': 'Manifest',
  'authored.editor.skillMd': 'SKILL.md',
  'authored.editor.prompts': 'Prompts',
  'authored.editor.validate': 'Validation',
  'authored.editor.savedraft': 'Save draft',
  'authored.editor.publish': 'Publish',
  'authored.editor.title': 'Compose skill',
  'authored.editor.description':
    'Edit the manifest on the left, files in the centre, and live validation on the right.',
  'authored.validation.valid': 'Valid manifest',
  'authored.validation.errors': '{n} violation(s)',
  'authored.validation.line': 'line {line} col {col}',
  'authored.versions.title': 'Versions',
  'authored.toast.saved': 'Draft saved',
  'authored.toast.published': 'Published',
  'authored.toast.archived': 'Archived',

  // Sub-nav
  'nav.sources': 'Sources',
  'nav.authored': 'Authored',

  // Command palette
  'cmdk.placeholder': 'Type a command or search…',
  'cmdk.section.navigate': 'Navigate',
  'cmdk.section.actions': 'Actions',
  'cmdk.action.toggleTheme': 'Toggle theme',
  'cmdk.action.toggleLocale': 'Toggle language',
  'cmdk.action.refresh': 'Refresh page',
  'cmdk.empty': 'No matches'
};
