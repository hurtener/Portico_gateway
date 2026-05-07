// Phase 8 storage seam — tenant-scoped CRUD for external skill sources
// (Git, HTTP, LocalDir, Authored) and for in-Portico authored skills.
//
// Concrete drivers (sqlite) implement these interfaces; the SkillSources
// + AuthoredSkills stores are surfaced through the Backend interface and
// consumed by the source registry + REST handlers.

package ifaces

import (
	"context"
	"time"
)

// SkillSourceRecord is one row of tenant_skill_sources. The
// driver-specific config travels as opaque JSON; each driver decodes
// its own shape inside its factory.
type SkillSourceRecord struct {
	TenantID       string
	Name           string
	Driver         string
	ConfigJSON     []byte
	CredentialRef  string
	RefreshSeconds int
	Priority       int
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastRefreshAt  *time.Time
	LastError      string
}

// SkillSourceStore persists tenant_skill_sources rows. Every method is
// tenant-scoped per CLAUDE.md §6.
type SkillSourceStore interface {
	Upsert(ctx context.Context, r *SkillSourceRecord) error
	Get(ctx context.Context, tenantID, name string) (*SkillSourceRecord, error)
	List(ctx context.Context, tenantID string) ([]*SkillSourceRecord, error)
	Delete(ctx context.Context, tenantID, name string) error

	// MarkRefreshed updates last_refresh_at and last_error in a single
	// row write. errStr is "" on success.
	MarkRefreshed(ctx context.Context, tenantID, name string, when time.Time, errStr string) error
}

// AuthoredSkillRecord is one row of tenant_authored_skills.
type AuthoredSkillRecord struct {
	TenantID     string
	SkillID      string
	Version      string
	Status       string // "draft" | "published" | "archived"
	ManifestJSON []byte
	Checksum     string
	AuthorUserID string
	CreatedAt    time.Time
	PublishedAt  *time.Time
	ArchivedAt   *time.Time
}

// AuthoredFileRecord is one row of tenant_authored_skill_files.
type AuthoredFileRecord struct {
	RelPath  string
	MIMEType string
	Contents []byte
}

// AuthoredSkillStore persists authored skill rows + their files.
type AuthoredSkillStore interface {
	// CreateDraft inserts a new draft revision and its file payloads
	// in a single transaction.
	CreateDraft(ctx context.Context, rec *AuthoredSkillRecord, files []AuthoredFileRecord) error

	// UpdateDraft swaps the manifest + files for an existing draft
	// (rejects when status != 'draft').
	UpdateDraft(ctx context.Context, rec *AuthoredSkillRecord, files []AuthoredFileRecord) error

	// Publish flips status='draft' → 'published' and updates the
	// active_skill pointer in a single transaction. ErrNotFound when
	// the row is missing or already published.
	Publish(ctx context.Context, tenantID, skillID, version string, when time.Time) (*AuthoredSkillRecord, error)

	// Archive transitions a published version to 'archived'. Removes
	// the active pointer if it referenced this version.
	Archive(ctx context.Context, tenantID, skillID, version string, when time.Time) error

	// DeleteDraft removes a draft + its file rows. Refuses to delete
	// a published version.
	DeleteDraft(ctx context.Context, tenantID, skillID, version string) error

	Get(ctx context.Context, tenantID, skillID, version string) (*AuthoredSkillRecord, []AuthoredFileRecord, error)
	History(ctx context.Context, tenantID, skillID string) ([]*AuthoredSkillRecord, error)
	ListAuthored(ctx context.Context, tenantID string) ([]*AuthoredSkillRecord, error)

	// ActiveVersion returns the currently active version string for
	// a skill, or ("", ErrNotFound) when no version is published.
	ActiveVersion(ctx context.Context, tenantID, skillID string) (string, error)

	// ListPublished returns every (skill_id, active_version) pair for
	// the tenant — the list the authored Source.List materialises.
	ListPublished(ctx context.Context, tenantID string) ([]AuthoredSkillRecord, error)
}
