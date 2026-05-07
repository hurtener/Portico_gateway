/**
 * Spanish locale. Mirrors en.ts key-for-key. CI runs an i18n-check to
 * enforce parity.
 */
export default {
  // Brand
  'brand.name': 'Portico',
  'brand.tagline': 'Una pasarela controlada para servidores MCP',

  // Common UI
  'common.refresh': 'Actualizar',
  'common.search': 'Buscar',
  'common.save': 'Guardar',
  'common.cancel': 'Cancelar',
  'common.delete': 'Eliminar',
  'common.enable': 'Habilitar',
  'common.disable': 'Deshabilitar',
  'common.close': 'Cerrar',
  'common.loading': 'Cargando…',
  'common.empty': 'Sin elementos.',
  'common.yes': 'sí',
  'common.no': 'no',
  'common.copy': 'Copiar',
  'common.copied': 'Copiado',
  'common.expand': 'Expandir',
  'common.collapse': 'Contraer',
  'common.loadMore': 'Cargar más',
  'common.viewAll': 'Ver todo',
  'common.dash': '—',

  // Navigation groups + items
  'nav.overview': 'Inicio',
  'nav.section.catalog': 'Catálogo',
  'nav.section.operations': 'Operaciones',
  'nav.section.admin': 'Administración',
  'nav.servers': 'Servidores',
  'nav.resources': 'Recursos',
  'nav.prompts': 'Prompts',
  'nav.apps': 'Aplicaciones',
  'nav.skills': 'Skills',
  'nav.sessions': 'Sesiones',
  'nav.approvals': 'Aprobaciones',
  'nav.audit': 'Auditoría',
  'nav.snapshots': 'Snapshots',
  'nav.secrets': 'Secretos',

  // TopBar / chrome
  'topbar.search': 'Buscar… (⌘K)',
  'topbar.notifications': 'Notificaciones',
  'topbar.notifications.empty': 'Sin notificaciones nuevas',
  'topbar.notifications.markRead': 'Marcar todo como leído',
  'topbar.profile.role.admin': 'Admin (local)',
  'topbar.profile.signOut': 'Cerrar sesión',
  'topbar.profile.signOut.disabled': 'La autenticación llega en la fase 12.',
  'topbar.theme.light': 'Claro',
  'topbar.theme.dark': 'Oscuro',
  'topbar.theme.system': 'Sistema',
  'topbar.locale.en': 'EN',
  'topbar.locale.es': 'ES',
  'topbar.envBadge': 'Local · dev',

  // Sidebar status
  'sidebar.health': 'Salud',
  'sidebar.ready': 'Listo',
  'sidebar.collapse': 'Contraer barra lateral',
  'sidebar.expand': 'Expandir barra lateral',

  // Landing page
  'landing.title': 'Consola de Portico',
  'landing.lede':
    'Pasarela multi-tenant y runtime de Skills para el Model Context Protocol. La consola es la superficie del operador para gestionar servidores, autorar skills e inspeccionar sesiones.',
  'landing.tile.health': 'Salud',
  'landing.tile.ready': 'Listo',
  'landing.tile.sessions': 'Sesiones activas',
  'landing.tile.approvals': 'Aprobaciones pendientes',
  'landing.tile.lastSnapshot': 'Último snapshot',
  'landing.tile.drift24h': 'Eventos de drift (24 h)',
  'landing.section.recentSessions': 'Sesiones recientes',
  'landing.section.recentApprovals': 'Aprobaciones recientes',
  'landing.section.recentAudit': 'Eventos de auditoría recientes',
  'landing.empty.sessions': 'Aún no hay sesiones.',
  'landing.empty.approvals': 'Sin aprobaciones pendientes.',
  'landing.empty.audit': 'Sin eventos recientes.',
  'landing.status.ok': 'ok',
  'landing.status.down': 'caído',
  'landing.status.pending': 'pendiente',
  'landing.relTime.never': 'nunca',
  'landing.relTime.justNow': 'ahora',
  'landing.relTime.minutes': 'hace {n} min',
  'landing.relTime.hours': 'hace {n} h',
  'landing.relTime.days': 'hace {n} d',

  // Servers
  'servers.title': 'Servidores',
  'servers.description':
    'Servidores MCP descendentes registrados para este tenant. Habilita, abre detalles o actualiza el estado en vivo.',
  'servers.col.id': 'ID',
  'servers.col.displayName': 'Nombre',
  'servers.col.transport': 'Transporte',
  'servers.col.mode': 'Modo',
  'servers.col.status': 'Estado',
  'servers.col.enabled': 'Activo',
  'servers.empty.title': 'Sin servidores registrados',
  'servers.empty.description':
    'Cuando un operador registre servidores MCP descendentes, aparecerán aquí.',

  // Server detail
  'serverDetail.spec': 'Especificación',
  'serverDetail.instances': 'Instancias ({count})',
  'serverDetail.instances.empty.title': 'Sin instancias activas',
  'serverDetail.instances.empty.description':
    'El supervisor lanzará una instancia cuando una sesión necesite este servidor.',
  'serverDetail.notFound.title': 'Servidor no encontrado',
  'serverDetail.notFound.description': 'No existe un servidor con id {id}.',
  'serverDetail.action.reload': 'Drenar y recargar',
  'serverDetail.field.transport': 'Transporte',
  'serverDetail.field.mode': 'Modo de runtime',
  'serverDetail.field.status': 'Estado',
  'serverDetail.field.enabled': 'Activo',
  'serverDetail.field.command': 'Comando',
  'serverDetail.field.args': 'Argumentos',
  'serverDetail.field.url': 'URL',

  // Resources
  'resources.title': 'Recursos',
  'resources.description':
    'Cada recurso que la pasarela agrega entre los servidores MCP descendentes.',
  'resources.col.server': 'Servidor',
  'resources.col.uri': 'URI',
  'resources.col.name': 'Nombre',
  'resources.col.mime': 'MIME',
  'resources.empty.title': 'Sin recursos descubiertos',
  'resources.empty.description':
    'Cuando un servidor descendente registre plantillas de recursos, aparecerán aquí.',

  // Prompts
  'prompts.title': 'Prompts',
  'prompts.description':
    'Prompts definidos por servidores disponibles para los clientes. Los argumentos requeridos están resaltados.',
  'prompts.col.server': 'Servidor',
  'prompts.col.name': 'Nombre',
  'prompts.col.description': 'Descripción',
  'prompts.col.arguments': 'Argumentos',
  'prompts.empty.title': 'Sin prompts disponibles',
  'prompts.empty.description': 'Cuando los servidores expongan prompts, aparecerán aquí.',

  // Apps
  'apps.title': 'Aplicaciones MCP',
  'apps.description':
    'Cada recurso ui:// descubierto entre los servidores descendentes. La fase 5 añade filtrado por política; el render previo llega más adelante en modo dev.',
  'apps.empty.title': 'Aún no se descubrieron recursos de aplicaciones MCP',
  'apps.empty.description':
    'Los servidores que expongan recursos ui:// aparecerán aquí automáticamente.',
  'apps.field.server': 'Servidor',
  'apps.field.uri': 'URI',
  'apps.field.upstream': 'Upstream',

  // Skills
  'skills.title': 'Skills',
  'skills.description': 'Skill Packs cargados y su estado de habilitación por tenant.',
  'skills.col.id': 'ID',
  'skills.col.title': 'Título',
  'skills.col.version': 'Versión',
  'skills.col.status': 'Estado',
  'skills.col.missing': 'Tools faltantes',
  'skills.status.enabled': 'habilitado',
  'skills.status.disabled': 'deshabilitado',
  'skills.status.missingTools': 'tools faltantes',
  'skills.empty.title': 'Sin skills cargados',
  'skills.empty.description':
    'Configura skills.sources en portico.yaml o instala skills desde la página de fuentes (fase 8).',

  // Skill detail
  'skillDetail.action.enableForTenant': 'Habilitar para el tenant',
  'skillDetail.action.disableForTenant': 'Deshabilitar para el tenant',
  'skillDetail.warnings': 'Advertencias',
  'skillDetail.manifest': 'Manifiesto',
  'skillDetail.notFound': 'Skill no encontrado.',

  // Sessions
  'sessions.title': 'Sesiones',
  'sessions.description':
    'Sesiones activas de la pasarela, las herramientas que invocaron y sus líneas de tiempo de replay.',
  'sessions.empty.title': 'Telemetría de sesiones en camino',
  'sessions.empty.description':
    'La fase 6 conecta el feed de sesiones en vivo. La línea de replay llega con la ruta del inspector en la fase 11.',

  // Approvals
  'approvals.title': 'Aprobaciones pendientes',
  'approvals.description':
    'Llamadas a herramientas y elicitations esperando una decisión del operador. Se actualiza cada 2 segundos.',
  'approvals.col.tool': 'Herramienta',
  'approvals.col.risk': 'Riesgo',
  'approvals.col.session': 'Sesión',
  'approvals.col.created': 'Creado',
  'approvals.col.expires': 'Expira',
  'approvals.action.approve': 'Aprobar',
  'approvals.action.deny': 'Denegar',
  'approvals.empty.title': 'Sin aprobaciones pendientes',
  'approvals.empty.description':
    'Cuando alguna herramienta requiera revisión del operador, aparecerá aquí.',

  // Audit
  'audit.title': 'Registro de auditoría',
  'audit.description':
    'Eventos de auditoría con redacción, scope por tenant. Filtra por slug; los payloads están codificados como JSON.',
  'audit.col.when': 'Cuándo',
  'audit.col.type': 'Tipo',
  'audit.col.tenant': 'Tenant',
  'audit.col.session': 'Sesión',
  'audit.col.payload': 'Payload',
  'audit.filter.placeholder': 'tipo de evento (p. ej. tool_call.complete)',
  'audit.empty.title': 'Ningún evento coincide',
  'audit.empty.description':
    'Prueba con un filtro más amplio, o ejecuta una sesión a través de la pasarela para poblar el registro.',

  // Snapshots
  'snapshots.title': 'Snapshots de catálogo',
  'snapshots.description':
    'Snapshots de catálogo por sesión — usados para replay y detección de drift.',
  'snapshots.col.id': 'ID',
  'snapshots.col.tenant': 'Tenant',
  'snapshots.col.session': 'Sesión',
  'snapshots.col.tools': 'Tools',
  'snapshots.col.created': 'Creado',
  'snapshots.col.hash': 'Hash',
  'snapshots.empty.title': 'Aún no hay snapshots',
  'snapshots.empty.description':
    'Crea una sesión contra la pasarela para materializar un snapshot del catálogo.',

  // Snapshot detail
  'snapshotDetail.title': 'Snapshot {id}',
  'snapshotDetail.field.tenant': 'Tenant',
  'snapshotDetail.field.session': 'Sesión',
  'snapshotDetail.field.created': 'Creado',
  'snapshotDetail.field.hash': 'Hash global',
  'snapshotDetail.warnings': 'Advertencias',
  'snapshotDetail.servers': 'Servidores ({count})',
  'snapshotDetail.tools': 'Tools ({count})',
  'snapshotDetail.skills': 'Skills ({count})',
  'snapshotDetail.notFound.title': 'Snapshot no encontrado',
  'snapshotDetail.notFound.description': 'No existe un snapshot con id {id}.',

  // Snapshot diff
  'snapshotDiff.title': 'Diferencia de snapshots',
  'snapshotDiff.description': 'Comparando {a} → {b}',
  'snapshotDiff.empty.title': 'Sin cambios',
  'snapshotDiff.empty.description': 'Ambos snapshots resuelven la misma huella canónica.',
  'snapshotDiff.section.tools': 'Tools',
  'snapshotDiff.section.resources': 'Recursos',
  'snapshotDiff.section.prompts': 'Prompts',
  'snapshotDiff.section.skills': 'Skills',
  'snapshotDiff.added': 'Agregados',
  'snapshotDiff.removed': 'Eliminados',
  'snapshotDiff.modified': 'Modificados',
  'snapshotDiff.fieldsChanged': 'campos modificados: {fields}',
  'snapshotDiff.noChanges.tools': 'Sin cambios en tools.',
  'snapshotDiff.noChanges.resources': 'Sin cambios en recursos.',
  'snapshotDiff.noChanges.prompts': 'Sin cambios en prompts.',
  'snapshotDiff.noChanges.skills': 'Sin cambios en skills.',

  // Secrets (admin)
  'secrets.title': 'Secretos del vault (admin)',
  'secrets.description':
    'Referencias a secretos cifrados con AES-256-GCM, indexadas por (tenant, nombre). Los valores nunca se muestran; solo aparecen las referencias en esta lista.',
  'secrets.form.title': 'Agregar secreto',
  'secrets.form.tenant': 'Tenant',
  'secrets.form.tenant.placeholder': 'acme',
  'secrets.form.name': 'Nombre',
  'secrets.form.name.placeholder': 'github_token',
  'secrets.form.value': 'Valor',
  'secrets.form.value.placeholder': '•••••••••',
  'secrets.form.required': 'tenant, nombre y valor son obligatorios',
  'secrets.col.tenant': 'Tenant',
  'secrets.col.name': 'Nombre',
  'secrets.section.existing': 'Secretos existentes',
  'secrets.toast.saved.title': 'Secreto guardado',
  'secrets.toast.saved.description': '{tenant}/{name} cifrado y almacenado.',
  'secrets.toast.saveFailed.title': 'Error al guardar',
  'secrets.toast.deleted.title': 'Secreto eliminado',
  'secrets.toast.deleted.description': '{tenant}/{name} purgado del vault.',
  'secrets.toast.deleteFailed.title': 'Error al eliminar',
  'secrets.confirmDelete': '¿Eliminar {tenant}/{name}? Esto no se puede deshacer.',
  'secrets.empty.title': 'Sin secretos almacenados',
  'secrets.empty.description':
    'Agrega una referencia de credencial arriba para vincularla a la estrategia de auth de un servidor.',
  'secrets.unavailable.title': 'Vault no configurado',
  'secrets.unavailable.description':
    'Configura PORTICO_VAULT_KEY antes de iniciar Portico para habilitar la gestión de secretos. El vault permanece deshabilitado en modo dev sin clave.',

  // Command palette
  'cmdk.placeholder': 'Escribe un comando o busca…',
  'cmdk.section.navigate': 'Navegar',
  'cmdk.section.actions': 'Acciones',
  'cmdk.action.toggleTheme': 'Cambiar tema',
  'cmdk.action.toggleLocale': 'Cambiar idioma',
  'cmdk.action.refresh': 'Recargar página',
  'cmdk.empty': 'Sin coincidencias'
};
