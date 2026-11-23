package engine_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestRecentPromptsAreTheProjectsFirstPromptsNewestFirst(t *testing.T) {
	c, _ := enginetest.Start(t)
	shop, blog := repo(t, "shop"), repo(t, "blog")
	reg := func(harness, id, folder, prompt string) engine.Session {
		t.Helper()
		s, err := c.RegisterSession(engine.RegisterSession{Harness: harness, ID: id, Folder: folder, FirstPrompt: prompt})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := reg("claude-code", "a", shop, "Fix the checkout total")
	reg("codex", "b", shop, "  Add a coupon field  ")
	reg("claude-code", "c", blog, "Write the about page")
	reg("claude-code", "d", shop, "")
	reg("claude-code", "e", shop, "Fix the checkout total")

	got, err := c.RecentPrompts(first.Project)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Fix the checkout total", "Add a coupon field"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prompts %q, want %q", got, want)
	}
}

func TestRecentPromptsStopAtTheLimit(t *testing.T) {
	c, _ := enginetest.Start(t)
	shop := repo(t, "shop")
	var project string
	for i := range engine.RecentPromptsLimit + 5 {
		s, err := c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: fmt.Sprint(i), Folder: shop, FirstPrompt: fmt.Sprint("prompt ", i)})
		if err != nil {
			t.Fatal(err)
		}
		project = s.Project
	}
	got, err := c.RecentPrompts(project)
	if err != nil || len(got) != engine.RecentPromptsLimit {
		t.Fatalf("%d prompts, %v", len(got), err)
	}
}

func TestRecentPromptsNeedAProject(t *testing.T) {
	c, _ := enginetest.Start(t)
	if _, err := c.RecentPrompts(""); !isStatus(err, 400) {
		t.Fatalf("got %v", err)
	}
}
