package pongcmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// A Note carries Resources as a Ping does.
func TestMCPNoteCarriesResources(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		text, s, isErr := call(t, conn, "send_note", map[string]any{"text": "The preview is deployed.",
			"resources": []map[string]any{{"title": "Preview deploy", "kind": "link", "content": "https://preview.example.com"}}})
		if isErr {
			t.Fatalf("send_note refused: %q", text)
		}
		n, err := c.Ping(s["ping_id"].(string))
		if err != nil || !n.IsNote || len(n.Resources) != 1 || n.Resources[0].Kind != engine.KindLink {
			t.Fatalf("Note %+v, %v", n, err)
		}
		b, ctype, err := c.Resource(n.ID, n.Resources[0].ID)
		if err != nil || string(b) != "https://preview.example.com" || ctype != "text/uri-list" {
			t.Fatalf("resource %q %q %v", b, ctype, err)
		}
	})
}

// send_ping marks finished work with finished; a Ping that neither blocks nor
// is finished is refused with what to do instead.
func TestMCPFinishedPing(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		text, _, isErr := call(t, conn, "send_ping", map[string]any{"question": "Which port should the dev server use?"})
		if !isErr || !strings.Contains(text, "set blocks_agent for a question, finished for finished work, or use send_note for news") {
			t.Fatalf("send = %v %q", isErr, text)
		}
		_, s, isErr := call(t, conn, "send_ping", map[string]any{"question": "Added the total; tests not run.", "finished": true})
		if isErr {
			t.Fatalf("finished Ping refused: %v", s)
		}
		p, _ := c.Ping(s["ping_id"].(string))
		if !p.Finished || p.Answers[len(p.Answers)-1].Label != engine.NothingMore {
			t.Fatalf("Ping = %+v", p)
		}
	})
}

// An agent reading tools/list learns finished, every Resource kind, that a
// plan comes as a document, and that a Note takes Resources.
func TestMCPToolsDescribeTheShapes(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		tools := map[string]map[string]any{}
		for _, tool := range conn(t, "tools/list", nil)["tools"].([]any) {
			tool := tool.(map[string]any)
			tools[tool["name"].(string)] = tool
		}
		prop := func(tool, name string) map[string]any {
			p, _ := tools[tool]["inputSchema"].(map[string]any)["properties"].(map[string]any)[name].(map[string]any)
			return p
		}
		if f := prop("send_ping", "finished"); f["type"] != "boolean" || !strings.Contains(f["description"].(string), "finished work") {
			t.Fatalf("finished = %v", f)
		}
		// A finished Ping is marked by Pong, so no Tag should say so again.
		if f := prop("send_ping", "finished"); !strings.Contains(f["description"].(string), "no Tag saying it is finished") {
			t.Fatalf("finished does not rule out a finished Tag: %v", f)
		}
		if tg := prop("send_ping", "tags"); !strings.Contains(tg["description"].(string), "A finished Ping takes no kind Tag") {
			t.Fatalf("tags do not rule out a kind Tag on a finished Ping: %v", tg)
		}
		if a := prop("send_ping", "approves"); !strings.Contains(a["description"].(string), "document") {
			t.Fatalf("approves = %v", a)
		}
		want := []any{"image", "document", "sheet", "slides", "link", "change", "error", "output", "file", "text"}
		for _, tool := range []string{"send_ping", "send_note"} {
			rs := prop(tool, "resources")
			if rs == nil {
				t.Fatalf("%s has no resources", tool)
			}
			kind := rs["items"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)
			if !reflect.DeepEqual(kind["enum"], want) {
				t.Fatalf("%s kinds = %v", tool, kind["enum"])
			}
			for _, k := range []string{"sheet", "slides", "link", ".xlsx", ".pptx"} {
				if !strings.Contains(kind["description"].(string), k) {
					t.Fatalf("%s kind description does not explain %s: %q", tool, k, kind["description"])
				}
			}
		}
	})
}
