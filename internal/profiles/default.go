package profiles

// DefaultProfile synthesises the back-compat profile a principal gets when no
// profile is bound to it: the tenant's FULL surface (every registered server,
// tool, Skill, and alias). Operators opt into restriction by creating a profile
// and binding a principal to it; they never have to opt out of a restriction
// they didn't ask for. Every Allows* method returns true for it.
func DefaultProfile(tenantID string) *Profile {
	return &Profile{
		TenantID:  tenantID,
		ID:        "default",
		Name:      "default",
		IsDefault: true,
	}
}
