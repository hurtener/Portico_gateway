package runtime

import (
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

func mkSkill(id, version string, plans []string) *Skill {
	return &Skill{
		Manifest: &manifest.Manifest{
			ID:      id,
			Version: version,
			Spec:    manifest.SpecVersion,
			Binding: manifest.Binding{Entitlements: manifest.Entitlements{Plans: plans}},
		},
		LoadedAt: time.Now().UTC(),
	}
}

func TestCatalog_SetGetRemove(t *testing.T) {
	c := NewCatalog()
	c.Set(mkSkill("a.x", "1.0.0", nil))
	if got, ok := c.Get("a.x"); !ok || got.Manifest.Version != "1.0.0" {
		t.Errorf("Get miss")
	}
	c.Set(mkSkill("a.x", "1.1.0", nil))
	if got, _ := c.Get("a.x"); got.Manifest.Version != "1.1.0" {
		t.Errorf("expected upsert, got %q", got.Manifest.Version)
	}
	c.Remove("a.x")
	if _, ok := c.Get("a.x"); ok {
		t.Errorf("Remove failed")
	}
}

func TestCatalog_ForTenant_GlobMatch(t *testing.T) {
	c := NewCatalog()
	c.Set(mkSkill("github.code-review", "1.0.0", nil))
	c.Set(mkSkill("github.docs", "1.0.0", nil))
	c.Set(mkSkill("postgres.sql", "1.0.0", nil))

	got := c.ForTenant([]string{"github.*"}, "")
	if len(got) != 2 {
		t.Fatalf("github.* match got %d", len(got))
	}
	got = c.ForTenant([]string{"*"}, "")
	if len(got) != 3 {
		t.Errorf("* match got %d", len(got))
	}
}

func TestCatalog_ForTenant_PlanMismatch(t *testing.T) {
	c := NewCatalog()
	c.Set(mkSkill("a.pro-only", "1.0.0", []string{"pro", "enterprise"}))
	c.Set(mkSkill("a.free", "1.0.0", []string{"free"}))
	got := c.ForTenant([]string{"*"}, "free")
	if len(got) != 1 || got[0].Manifest.ID != "a.free" {
		t.Errorf("plan filter wrong: %+v", got)
	}
}

func TestCatalog_SubscribePublishOnSet(t *testing.T) {
	c := NewCatalog()
	ch := c.Subscribe()
	defer c.Unsubscribe(ch)
	c.Set(mkSkill("a.b", "1.0.0", nil))
	select {
	case ev := <-ch:
		if ev.Kind != ChangeAdded || ev.Skill.Manifest.ID != "a.b" {
			t.Errorf("unexpected: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("never published")
	}
}
