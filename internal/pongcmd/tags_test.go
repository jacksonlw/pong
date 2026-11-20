package pongcmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/tagicons"
)

var sentTags = []map[string]any{
	{"text": "prod", "icon": "server.rack", "colour": "rose"},
	{"text": "feat/login", "icon": "arrow.triangle.branch"},
	{"text": "LIN-123", "icon": "no.such.icon"},
	{"text": "api"},
}

var wantTags = []engine.Tag{
	{Text: "prod", Icon: "server.rack", Colour: "rose"},
	{Text: "feat/login", Icon: "arrow.triangle.branch"},
	{Text: "LIN-123", Icon: "no.such.icon"},
	{Text: "api"},
}

// createdTags returns the Tags on the ping.created event for id.
func createdTags(t *testing.T, c *client.Client, id string) []engine.Tag {
	t.Helper()
	evs, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-evs:
			var p engine.Ping
			if e.Kind == engine.EventPingCreated && json.Unmarshal(e.Data, &p) == nil && p.ID == id {
				return p.Tags
			}
		case <-timeout:
			t.Fatalf("no ping.created event for %s", id)
		}
	}
}

func TestMCPPingAndNoteCarryTags(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		for tool, args := range map[string]map[string]any{
			"send_ping": {"question": "Run the migration?", "blocks_agent": true, "tags": sentTags},
			"send_note": {"text": "Migration ran.", "tags": sentTags},
		} {
			text, s, isErr := call(t, conn, tool, args)
			if isErr {
				t.Fatalf("%s refused: %q", tool, text)
			}
			id := s["ping_id"].(string)
			p, _ := c.Ping(id)
			if !reflect.DeepEqual(p.Tags, wantTags) {
				t.Fatalf("%s: API Tags = %+v", tool, p.Tags)
			}
			if got := createdTags(t, c, id); !reflect.DeepEqual(got, wantTags) {
				t.Fatalf("%s: event Tags = %+v", tool, got)
			}
		}
	})
}

func TestSendToolsOfferTags(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		offered := map[string]bool{}
		for _, tool := range conn(t, "tools/list", nil)["tools"].([]any) {
			tool := tool.(map[string]any)
			tags, ok := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)["tags"].(map[string]any)
			if !ok {
				continue
			}
			item := tags["items"].(map[string]any)
			var fields []string
			for _, f := range []string{"text", "icon", "colour"} {
				if _, ok := item["properties"].(map[string]any)[f]; ok {
					fields = append(fields, f)
				}
			}
			if _, linked := item["properties"].(map[string]any)["link"]; linked || len(fields) != 3 || !reflect.DeepEqual(item["required"], []any{"text"}) {
				t.Fatalf("%s tags = %v", tool["name"], tags)
			}
			offered[tool["name"].(string)] = true
		}
		if !offered["send_ping"] || !offered["send_note"] || len(offered) != 2 {
			t.Fatalf("tools offering tags = %v", offered)
		}
	})
}

func TestPingCommandSendsTags(t *testing.T) {
	c, dir := enginetest.Start(t)
	var out, errOut bytes.Buffer
	code := run([]string{"ping", "-dir", dir, "-session", "s1", "-note", "-question", "Migration ran.",
		"-tag", "prod|server.rack|rose", "-tag", "feat/login|arrow.triangle.branch",
		"-tag", "LIN-123|no.such.icon", "-tag", "api"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	ps, _ := c.Pings("")
	if len(ps) != 1 || !reflect.DeepEqual(ps[0].Tags, wantTags) {
		t.Fatalf("Pings = %+v", ps)
	}

	errOut.Reset()
	code = run([]string{"ping", "-dir", dir, "-session", "s1", "-note", "-question", "Done.", "-tag", "notes|folder||https://example.com"}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), `tag 1 "notes" has a link, but Tags are never clicked`) {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}

func TestMCPRefusesBadTags(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcd" // 30 characters
	cases := []struct {
		name string
		tag  map[string]any
		want string // in the fix-it message; "" means accepted
	}{
		{"empty text", map[string]any{"text": "  ", "icon": "cloud"}, `tag 2 has no text: give it text of 1 to 30 characters`},
		{"31 characters", map[string]any{"text": long + "e"}, `tag 2 "` + long + `e" has 31 characters: shorten it to at most 30`},
		{"30 characters", map[string]any{"text": long}, ""},
		{"30 characters not bytes", map[string]any{"text": "ééééééééééééééééééééééééééééé✓"}, ""},
		{"unknown icon and colour", map[string]any{"text": "x", "icon": "no.such.icon", "colour": "chartreuse"}, ""},
		{"any link", map[string]any{"text": "docs", "link": "https://example.com/pr/12"}, `tag 2 "docs" has a link, but Tags are never clicked: drop link from the Tag and attach https://example.com/pr/12 as a Resource of kind link`},
		{"app link", map[string]any{"text": "editor", "link": "vscode://file/tmp/project"}, `has a link, but Tags are never clicked`},
		{"31 characters inside spaces", map[string]any{"text": "  " + long + "e  "}, `tag 2 "` + long + `e" has 31 characters`},
		{"30 characters inside spaces", map[string]any{"text": "  " + long + "  "}, ""},
	}
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		for _, tc := range cases {
			for _, tool := range []string{"send_ping", "send_note"} {
				args := map[string]any{"tags": []map[string]any{{"text": "prod"}, tc.tag}}
				if tool == "send_note" {
					args["text"] = "Deployed."
				} else {
					args["question"], args["blocks_agent"] = "Deploy?", true
				}
				text, _, isErr := call(t, conn, tool, args)
				if tc.want == "" {
					if isErr {
						t.Errorf("%s %s refused: %q", tc.name, tool, text)
					}
				} else if !isErr || !strings.Contains(text, tc.want) {
					t.Errorf("%s %s = %v %q, want %q", tc.name, tool, isErr, text, tc.want)
				}
			}
		}
	})
}

// TestMCPToolsListTagIconsAndColours checks that an agent reading tools/list
// learns every icon and pastel in the brand kit.
func TestMCPToolsListTagIconsAndColours(t *testing.T) {
	c, _ := enginetest.Start(t)
	conn := overHTTP(c, "s1")
	words := map[string]map[string]bool{}
	for _, tool := range conn(t, "tools/list", nil)["tools"].([]any) {
		m := tool.(map[string]any)
		schema, _ := json.Marshal(m["inputSchema"].(map[string]any)["properties"].(map[string]any)["tags"])
		set := map[string]bool{}
		for _, w := range strings.FieldsFunc(string(schema), func(r rune) bool {
			return !(r == '.' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) {
			set[strings.Trim(w, ".")] = true
		}
		words[m["name"].(string)] = set
	}
	for _, name := range []string{"send_ping", "send_note"} {
		for _, want := range append(append([]string{}, tagicons.Icons...), tagicons.Colours...) {
			if !words[name][want] {
				t.Errorf("%s's tags description does not name %q", name, want)
			}
		}
	}
}
