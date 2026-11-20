package engine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestCapturedPingDropsBadTagsAndIsAccepted(t *testing.T) {
	c, _ := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{Session: "s1", Question: "Allow the edit?", Captured: true, BlocksAgent: true,
		Tags: []engine.Tag{
			{Text: "prod", Icon: "server.rack"},
			{Text: ""},
			{Text: strings.Repeat("x", 31)},
			{Text: "notes", Link: "file:///etc/passwd"},
			{Text: "docs", Link: "not a url"},
			{Text: "api", Colour: "mint"},
		}})
	if err != nil {
		t.Fatalf("captured Ping refused: %v", err)
	}
	want := []engine.Tag{{Text: "prod", Icon: "server.rack"}, {Text: "api", Colour: "mint"}}
	if got, _ := c.Ping(p.ID); !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("Tags = %+v, want %+v", got.Tags, want)
	}
}

func TestTagsNeverChangeQueueOrder(t *testing.T) {
	c, _ := enginetest.Start(t)
	many := make([]engine.Tag, 50) // no limit on the number of Tags
	for i := range many {
		many[i] = engine.Tag{Text: "prod", Icon: "server.rack", Colour: "rose"}
	}
	var ids []string
	for _, tags := range [][]engine.Tag{nil, many, nil} {
		p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Deploy?", Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	ps, _ := c.Pings(engine.StateWaiting)
	var got []string
	for _, p := range ps {
		got = append(got, p.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("Queue = %v, want %v", got, ids)
	}
	if len(ps[1].Tags) != 50 {
		t.Fatalf("kept %d Tags, want 50", len(ps[1].Tags))
	}
}

// send is one Tag sent on a Ping from a Project, and the colour the Tag must
// come back with.
type send struct {
	project  string // "a" or "b"
	tag      engine.Tag
	restart  bool // restart the Engine before sending
	captured bool
	want     string
}

func TestATagKeepsItsColourInAProject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sends []send
	}{
		{"a later Tag takes the first colour, whatever it sends", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "mint"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod"}, want: "rose"},
		}},
		{"text is compared without regard to case", []send{
			{project: "a", tag: engine.Tag{Text: "Prod", Colour: "rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "PROD", Colour: "mint"}, want: "rose"},
		}},
		{"a text never given a colour stays neutral until one is given, which then sticks", []send{
			{project: "a", tag: engine.Tag{Text: "staging"}, want: ""},
			{project: "a", tag: engine.Tag{Text: "staging"}, want: ""},
			{project: "a", tag: engine.Tag{Text: "staging", Colour: "sky"}, want: "sky"},
			{project: "a", tag: engine.Tag{Text: "staging", Colour: "mint"}, want: "sky"},
			{project: "a", tag: engine.Tag{Text: "staging"}, want: "sky"},
		}},
		{"another Project starts fresh", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "rose"}, want: "rose"},
			{project: "b", tag: engine.Tag{Text: "prod"}, want: ""},
			{project: "b", tag: engine.Tag{Text: "prod", Colour: "mint"}, want: "mint"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "mint"}, want: "rose"},
		}},
		{"the memory survives restarting the Engine", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "mint"}, restart: true, want: "rose"},
		}},
		{"a colour that is not a pastel does not stick", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "chartreuse"}, want: ""},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "#ff0000"}, want: ""},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "chartreuse"}, want: "rose"},
		}},
		{"a pastel is compared and kept lower-cased", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "Rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "MINT"}, want: "rose"},
		}},
		{"a captured Ping follows the same rule", []send{
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "rose"}, want: "rose"},
			{project: "a", tag: engine.Tag{Text: "prod", Colour: "mint"}, captured: true, want: "rose"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, dir := enginetest.Start(t)
			folders := map[string]string{"a": t.TempDir(), "b": t.TempDir()}
			for i, s := range tc.sends {
				if s.restart {
					c = enginetest.Restart(t, dir)
				}
				p, err := c.CreatePing(client.NewPing{Session: "s-" + s.project, Folder: folders[s.project], Question: "Deploy?", Captured: s.captured, BlocksAgent: s.captured, Finished: !s.captured, Tags: []engine.Tag{s.tag}})
				if err != nil {
					t.Fatal(err)
				}
				got, err := c.Ping(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Tags[0].Colour != s.want || p.Tags[0].Colour != s.want {
					t.Fatalf("send %d %q: colour = %q (returned %q), want %q", i+1, s.tag.Text, got.Tags[0].Colour, p.Tags[0].Colour, s.want)
				}
			}
		})
	}
}

func TestAFinishedPingDropsTagsThatSayFinished(t *testing.T) {
	c, _ := enginetest.Start(t)
	branch := engine.Tag{Text: "main", Icon: "arrow.triangle.branch"}
	p, err := c.CreatePing(client.NewPing{Session: "s1", Question: "The footer is live.", Finished: true,
		Tags: []engine.Tag{
			{Text: "Finished", Icon: "checkmark.seal", Colour: "mint"},
			{Text: "Finished work"},
			{Text: " done "},
			{Text: "Completed"},
			{Text: "Finished Pings", Icon: "checkmark.seal"},
			branch,
		}})
	if err != nil {
		t.Fatal(err)
	}
	want := []engine.Tag{{Text: "Finished Pings", Icon: "checkmark.seal"}, branch}
	if got, _ := c.Ping(p.ID); !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("Tags = %+v, want %+v", got.Tags, want)
	}
}

func TestAQuestionKeepsATagThatSaysFinished(t *testing.T) {
	c, _ := enginetest.Start(t)
	tags := []engine.Tag{{Text: "Done"}}
	p, err := c.CreatePing(client.NewPing{Session: "s1", Question: "Which column?", BlocksAgent: true, Tags: tags})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Ping(p.ID); !reflect.DeepEqual(got.Tags, tags) {
		t.Fatalf("Tags = %+v, want %+v", got.Tags, tags)
	}
}
