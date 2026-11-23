package engine_test

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

// skillHarness is one Harness, "fake", whose Skills are set by the test: the
// same user ones everywhere, and project ones by the folder's last name, or
// "*" for any folder.
type skillHarness struct {
	mu      *sync.Mutex
	user    []string
	project map[string][]string
	reads   *[]string
}

func newSkillHarness(user []string, project map[string][]string) skillHarness {
	return skillHarness{mu: &sync.Mutex{}, user: user, project: project, reads: &[]string{}}
}

func (skillHarness) List() []engine.HarnessStatus {
	return []engine.HarnessStatus{{ID: "fake", Name: "Fake", Installed: true, Connected: true}}
}
func (skillHarness) Repair(string) (engine.HarnessStatus, error) {
	return engine.HarnessStatus{}, engine.ErrNoHarness
}
func (h skillHarness) Skills(harness, root string) (user, project []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.reads = append(*h.reads, harness+" "+filepath.Base(root))
	if harness != "fake" {
		return nil, nil
	}
	if names, ok := h.project[filepath.Base(root)]; ok {
		return h.user, names
	}
	return h.user, h.project["*"]
}
func (h skillHarness) read(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range *h.reads {
		if r == "fake "+name {
			return true
		}
	}
	return false
}

func TestSkillsAreTheHarnesssOwnAndTheProjects(t *testing.T) {
	h := newSkillHarness([]string{"grilling", "pong:pong"}, map[string][]string{"shop": {"deploy", "grilling"}})
	c := enginetest.StartWith(t, engine.Config{Harnesses: h})
	shop := repo(t, "shop")
	p, err := c.ProjectFor(shop)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Skills("fake", p.ID, "", "")
	if want := []string{"deploy", "grilling", "pong:pong"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("project: %q %v, want %q", got, err, want)
	}
	got, err = c.Skills("fake", "", t.TempDir(), "")
	if want := []string{"grilling", "pong:pong"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("folder: %q %v, want %q", got, err, want)
	}
	if got, err = c.Skills("codex", p.ID, "", ""); err != nil || len(got) != 0 {
		t.Fatalf("a Harness with none: %q %v", got, err)
	}
	if _, err = c.Skills("", p.ID, "", ""); !isStatus(err, 400) {
		t.Fatalf("no Harness: %v", err)
	}
	if _, err = c.Skills("fake", p.ID, shop, ""); !isStatus(err, 400) {
		t.Fatalf("both a Project and a folder: %v", err)
	}
}

func TestSkillsAreReadAsASessionStartsInAProject(t *testing.T) {
	h := newSkillHarness(nil, nil)
	c := enginetest.StartWith(t, engine.Config{Harnesses: h})
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "a", Folder: repo(t, "blog")}); err != nil {
		t.Fatal(err)
	}
	enginetest.Eventually(t, "the blog's Skills to be read", func() bool { return h.read("blog") })
}

func TestSkillsReachOtherComputersWithTheirState(t *testing.T) {
	book := &enginetest.Book{}
	h := newSkillHarness([]string{"grilling"}, map[string][]string{"*": {"deploy"}})
	desk := enginetest.StartComputerWith(t, book, "Desktop", engine.Config{Harnesses: h})
	laptop := enginetest.StartComputerWith(t, book, "Laptop", engine.Config{Harnesses: harnessesOn{true, true, ""}})
	enginetest.Pair(t, desk, laptop)

	p, err := desk.C.ProjectFor(gitRepo(t, "git@github.com:me/shop.git"))
	if err != nil {
		t.Fatal(err)
	}
	desk.C.ChangeProject(p.ID, engine.ChangeProject{Enabled: ptr(true)})
	if _, err := desk.C.Skills("fake", p.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	var shop engine.Project
	var got []string
	enginetest.Eventually(t, "the laptop to know the desktop's Skills in the shop", func() bool {
		ps, _ := laptop.C.Projects()
		for _, x := range ps {
			if x.Key == p.Key {
				shop = x
			}
		}
		if shop.ID == "" {
			return false
		}
		got, _ = laptop.C.Skills("fake", shop.ID, "", "")
		return reflect.DeepEqual(got, []string{"deploy", "grilling"})
	})
	cs, err := laptop.C.Computers()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Name == "Desktop" && !reflect.DeepEqual(c.Harnesses[0].ProjectSkills[p.Key], []string{"deploy"}) {
			t.Fatalf("the desktop's state: %+v", c.Harnesses[0])
		}
	}
}
