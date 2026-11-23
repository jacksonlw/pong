package turn

import (
	"strings"
	"testing"

	"github.com/jacksonlw/pong/internal/engine"
)

// Closing messages below are real ones, from Sessions run without Pong in the
// experiment that compared Sessions with and without Pong, or shaped like them.

func TestAMessageThatAsksNothingIsFinished(t *testing.T) {
	msg := "Todos can now have due dates, and all 6 tests pass. A test run before I made changes had 1 failure out of 2 tests.\n\n**What changed:**\n- **Adding:** `add(todos, title, due=None)` takes an optional date.\n- **Listing:** `pending()` returns undone todos with the soonest due date first.\n\nNothing is committed."
	n := Build(msg, Facts{})
	if !n.Finished || n.BlocksAgent || !n.Captured {
		t.Fatalf("finished=%v blocks=%v captured=%v", n.Finished, n.BlocksAgent, n.Captured)
	}
	if n.Question != "Todos can now have due dates, and all 6 tests pass." {
		t.Errorf("question = %q", n.Question)
	}
	if len(n.Answers) != 0 {
		t.Errorf("answers = %v; the Engine adds Nothing more itself", n.Answers)
	}
	for _, want := range []string{"A test run before I made changes", "**What changed:**", "- **Adding:**", "Nothing is committed."} {
		if !strings.Contains(n.Context, want) {
			t.Errorf("context lacks %q:\n%s", want, n.Context)
		}
	}
	if strings.Contains(n.Context, "Todos can now have due dates") {
		t.Errorf("context repeats the question:\n%s", n.Context)
	}
}

func TestAnOfferWithoutAQuestionMarkAsksYesOrNo(t *testing.T) {
	msg := "I added a `python3 todo.py export [md|csv|json]` command.\n\n**The test suite still fails once, because of a bug that was already there.** I didn't change either one, but I can fix them if you'd like."
	n := Build(msg, Facts{})
	if n.Finished || !n.BlocksAgent {
		t.Fatalf("finished=%v blocks=%v", n.Finished, n.BlocksAgent)
	}
	if n.Question != "I didn't change either one, but I can fix them if you'd like." {
		t.Errorf("question = %q", n.Question)
	}
	if len(n.Answers) != 2 || n.Answers[0].Label != "Yes" || n.Answers[1].Label != "No" {
		t.Errorf("answers = %v", n.Answers)
	}
	if !strings.Contains(n.Context, "export [md|csv|json]") || !strings.Contains(n.Context, "already there.**") {
		t.Errorf("context = %q", n.Context)
	}
	if len(n.Tags) != 1 || n.Tags[0].Text != "Question" {
		t.Errorf("tags = %v", n.Tags)
	}
}

func TestAListAfterTheQuestionGivesTheAnswersRecommendedFirst(t *testing.T) {
	msg := "The fix is committed locally as `ce9f9a6`.\n\nShould I push it to origin?\n\n- **Keep it local for now**: you push it later\n- **Push to origin** (my pick): main gets the fix now"
	n := Build(msg, Facts{})
	if n.Question != "Should I push it to origin?" {
		t.Errorf("question = %q", n.Question)
	}
	want := []engine.Answer{{Label: "Push to origin", LeadsTo: "main gets the fix now"}, {Label: "Keep it local for now", LeadsTo: "you push it later"}}
	if len(n.Answers) != 2 || n.Answers[0] != want[0] || n.Answers[1] != want[1] {
		t.Errorf("answers = %#v", n.Answers)
	}
	if n.Recommended != "Push to origin" {
		t.Errorf("recommended = %q", n.Recommended)
	}
	if n.Context != "The fix is committed locally as `ce9f9a6`." {
		t.Errorf("context = %q", n.Context)
	}
}

func TestCodeTablesAndLinksBecomeResources(t *testing.T) {
	msg := "Both tests pass. Here is the test output:\n\n```\ntest_add ... ok\ntest_pending_skips_done ... ok\n```\n\nThe timings by step:\n\n| Step | Seconds |\n|---|---|\n| build | 12 |\n| test, all | 30 |\n\nThe docs are at https://code.claude.com/docs/en/channels."
	n := Build(msg, Facts{})
	if !n.Finished {
		t.Fatal("not finished")
	}
	got := map[string]engine.NewResource{}
	for _, r := range n.Resources {
		got[r.Kind] = r
	}
	if r := got[engine.KindOutput]; r.Title != "Test output" || !strings.Contains(r.Content, "test_add ... ok") {
		t.Errorf("output = %#v", r)
	}
	if r := got[engine.KindSheet]; r.Title != "Timings by step" || r.Content != "Step,Seconds\nbuild,12\n\"test, all\",30" {
		t.Errorf("sheet = %#v", r)
	}
	if r := got[engine.KindLink]; r.Content != "https://code.claude.com/docs/en/channels" || r.Title != "Code.claude.com channels" {
		t.Errorf("link = %#v", r)
	}
	if strings.Contains(n.Context, "test_add ... ok") || strings.Contains(n.Context, "| build |") {
		t.Errorf("context keeps what became a Resource:\n%s", n.Context)
	}
}

func TestTheTurnsTestOutputAndDocumentsAreAttached(t *testing.T) {
	n := Build("I wrote the release notes.", Facts{TestOutput: "ok  \tpong\t1.2s\n", Documents: []string{"/p/release-notes.md"}})
	var kinds []string
	for _, r := range n.Resources {
		kinds = append(kinds, r.Kind+":"+r.Title)
	}
	if strings.Join(kinds, ",") != "output:Test output,document:Release notes" {
		t.Errorf("resources = %v", kinds)
	}
}

func TestAnEmptyMessageStillSaysSomething(t *testing.T) {
	if n := Build("  \n", Facts{}); n.Question == "" || !n.Finished {
		t.Errorf("ping = %#v", n)
	}
}

func TestASentenceIntroducingAListEndsAsASentence(t *testing.T) {
	n := Build("I renamed `add` to `add_todo` in both places it's used:\n\n- `todo.py:20`, the definition\n- `todo.py:40`, in `main`\n\nThe tests pass.", Facts{})
	if !n.Finished || n.Question != "I renamed `add` to `add_todo` in both places it's used." {
		t.Fatalf("question = %q", n.Question)
	}
	if !strings.HasPrefix(n.Context, "- `todo.py:20`, the definition") || strings.Contains(n.Context, "both places it's used") {
		t.Fatalf("context = %q", n.Context)
	}
}

// Every Resource title is two to four words, as the Engine requires, however
// short the line that introduced it.
func TestResourceTitlesAreTwoToFourWords(t *testing.T) {
	msg := "Done.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nSee https://github.com and https://code.claude.com/docs/en/a/very/long/path/name/here for more.\n\n```\n```\n\n```\nx := 1\n```"
	n := Build(msg, Facts{Documents: []string{"/p/README.md"}})
	for _, r := range n.Resources {
		if w := len(strings.Fields(r.Title)); w < 2 || w > 4 {
			t.Errorf("%s title %q has %d words", r.Kind, r.Title, w)
		}
		if r.Kind != engine.KindDocument && strings.TrimSpace(r.Content) == "" {
			t.Errorf("empty %s resource %q", r.Kind, r.Title)
		}
	}
	if len(n.Resources) != 5 { // the table, two links, one code block, the document
		t.Errorf("resources = %+v", n.Resources)
	}
}

// A question mark in a link asks nothing.
func TestAQuestionMarkInALinkAsksNothing(t *testing.T) {
	if n := Build("Fixed it. See https://example.com/search?q=1 for details.", Facts{}); !n.Finished {
		t.Fatalf("a link made it a question: %+v", n)
	}
}
