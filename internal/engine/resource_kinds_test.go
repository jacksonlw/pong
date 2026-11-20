package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

type sheetJSON struct {
	Sheets []struct {
		Name string     `json:"name"`
		Rows [][]string `json:"rows"`
	} `json:"sheets"`
}

// sendOne sends a finished Ping with r from folder and returns the stored
// Resource with its content and content type.
func sendOne(t *testing.T, c *client.Client, folder string, r engine.NewResource) (engine.Resource, []byte, string) {
	t.Helper()
	p, err := c.CreatePing(client.NewPing{Session: "s1", Folder: folder, Question: "Report written.", Finished: true, Resources: []engine.NewResource{r}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 1 {
		t.Fatalf("resources %+v", p.Resources)
	}
	b, ctype, err := c.Resource(p.ID, p.Resources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return p.Resources[0], b, ctype
}

func sheets(t *testing.T, b []byte) sheetJSON {
	t.Helper()
	var s sheetJSON
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("not sheet JSON: %v: %s", err, b)
	}
	return s
}

func TestALinkIsStoredAsAURIList(t *testing.T) {
	c, _ := enginetest.Start(t)
	r, b, ctype := sendOne(t, c, t.TempDir(), engine.NewResource{Title: "Preview deploy", Kind: engine.KindLink, Content: " https://preview.example.com/pr/12 \n"})
	if r.Kind != engine.KindLink || ctype != "text/uri-list" || string(b) != "https://preview.example.com/pr/12" {
		t.Fatalf("link %+v %q %q", r, ctype, b)
	}
}

func TestCSVTSVAndInlineCSVBecomeOneSheet(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "totals.csv"), []byte("name,total\n\"Smith, J\",12\n"), 0o600)
	os.WriteFile(filepath.Join(folder, "totals.tsv"), []byte("name\ttotal\nSmith\t12\n"), 0o600)
	for name, tc := range map[string]struct {
		r    engine.NewResource
		rows [][]string
	}{
		"csv":        {engine.NewResource{Title: "Order totals", Kind: engine.KindSheet, File: "totals.csv"}, [][]string{{"name", "total"}, {"Smith, J", "12"}}},
		"tsv":        {engine.NewResource{Title: "Order totals", Kind: engine.KindSheet, File: "totals.tsv"}, [][]string{{"name", "total"}, {"Smith", "12"}}},
		"inline csv": {engine.NewResource{Title: "Order totals", Kind: engine.KindSheet, Content: "a,b\n1,2,3\n"}, [][]string{{"a", "b"}, {"1", "2", "3"}}},
	} {
		r, b, ctype := sendOne(t, c, folder, tc.r)
		if r.Kind != engine.KindSheet || ctype != engine.SheetContentType {
			t.Fatalf("%s: %+v %q", name, r, ctype)
		}
		s := sheets(t, b)
		if len(s.Sheets) != 1 || s.Sheets[0].Name != "Order totals" || !reflect.DeepEqual(s.Sheets[0].Rows, tc.rows) {
			t.Fatalf("%s: sheets %s", name, b)
		}
	}
}

func TestAnXLSXGivesOneSheetPerWorksheet(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Orders")
	f.SetSheetRow("Orders", "A1", &[]any{"item", "price"})
	f.SetSheetRow("Orders", "A2", &[]any{"pen", 1.5})
	f.NewSheet("Totals")
	f.SetCellValue("Totals", "A1", "sum")
	f.SetCellFormula("Totals", "B1", "Orders!B2*2")
	f.SetCellValue("Totals", "B1", 3) // the cached value a real spreadsheet app writes
	if err := f.SaveAs(filepath.Join(folder, "orders.xlsx")); err != nil {
		t.Fatal(err)
	}
	_, b, ctype := sendOne(t, c, folder, engine.NewResource{Title: "Order sheet", Kind: engine.KindSheet, File: "orders.xlsx"})
	s := sheets(t, b)
	if ctype != engine.SheetContentType || len(s.Sheets) != 2 || s.Sheets[0].Name != "Orders" || s.Sheets[1].Name != "Totals" {
		t.Fatalf("sheets %q %s", ctype, b)
	}
	if !reflect.DeepEqual(s.Sheets[0].Rows, [][]string{{"item", "price"}, {"pen", "1.5"}}) || !reflect.DeepEqual(s.Sheets[1].Rows, [][]string{{"sum", "3"}}) {
		t.Fatalf("rows %s", b)
	}
}

func TestSlidesFromAPDF(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "deck.pdf"), []byte("%PDF-1.4\n%fake\n"), 0o600)
	r, b, ctype := sendOne(t, c, folder, engine.NewResource{Title: "Launch deck", Kind: engine.KindSlides, File: "deck.pdf"})
	if r.Kind != engine.KindSlides || ctype != "application/pdf" || string(b) != "%PDF-1.4\n%fake\n" {
		t.Fatalf("slides %+v %q", r, ctype)
	}
}

// A pptx becomes slides only when soffice is found; otherwise it is kept as
// a file, and the Ping is never refused over it.
func TestAPPTXBecomesSlidesWhenSofficeIsFound(t *testing.T) {
	folder := t.TempDir()
	pptx := []byte("PK\x03\x04 not really a deck")
	os.WriteFile(filepath.Join(folder, "deck.pptx"), pptx, 0o600)
	deck := engine.NewResource{Title: "Launch deck", Kind: engine.KindSlides, File: "deck.pptx"}
	engine.SetSofficeApp(t, filepath.Join(t.TempDir(), "no-soffice"))

	t.Run("no soffice", func(t *testing.T) {
		t.Setenv("PATH", "")
		c, _ := enginetest.Start(t)
		r, b, ctype := sendOne(t, c, folder, deck)
		if r.Kind != engine.KindFile || string(b) != string(pptx) || ctype != engine.PPTXContentType {
			t.Fatalf("resource %+v %q", r, ctype)
		}
	})
	t.Run("soffice fails", func(t *testing.T) {
		bin := t.TempDir()
		os.WriteFile(filepath.Join(bin, "soffice"), []byte("#!/bin/sh\nexit 1\n"), 0o700)
		t.Setenv("PATH", bin)
		c, _ := enginetest.Start(t)
		if r, _, _ := sendOne(t, c, folder, deck); r.Kind != engine.KindFile {
			t.Fatalf("resource %+v", r)
		}
	})
	t.Run("fake soffice", func(t *testing.T) {
		bin := t.TempDir()
		// Writes <outdir>/<name>.pdf as soffice --headless --convert-to pdf --outdir DIR FILE does.
		script := "#!/bin/sh\nout=''\nwhile [ $# -gt 1 ]; do\n  if [ \"$1\" = --outdir ]; then out=$2; fi\n  shift\ndone\n" +
			"name=${1##*/}\nname=${name%%.pptx}\nprintf '%%PDF-1.4 converted' > \"$out/$name.pdf\"\n"
		os.WriteFile(filepath.Join(bin, "soffice"), []byte(script), 0o700)
		t.Setenv("PATH", bin)
		c, _ := enginetest.Start(t)
		r, b, ctype := sendOne(t, c, folder, deck)
		if r.Kind != engine.KindSlides || ctype != "application/pdf" || string(b) != "%PDF-1.4 converted" {
			t.Fatalf("resource %+v %q %q", r, ctype, b)
		}
	})
}

func TestBadNewResourcesAreRefused(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "broken.xlsx"), []byte("not a workbook"), 0o600)
	os.WriteFile(filepath.Join(folder, "deck.pdf"), []byte("not a pdf"), 0o600)
	os.WriteFile(filepath.Join(folder, "notes.md"), []byte("# notes"), 0o600)
	os.WriteFile(filepath.Join(folder, "deck.key"), []byte("keynote"), 0o600)
	for name, tc := range map[string]struct {
		r    engine.NewResource
		says string
	}{
		"inline slides":       {engine.NewResource{Title: "Launch deck", Kind: engine.KindSlides, Content: "slide one"}, `resource "Launch deck" is slides, which need a file`},
		"slides not pdf":      {engine.NewResource{Title: "Launch deck", Kind: engine.KindSlides, File: "deck.key"}, `resource "Launch deck" is slides from deck.key: attach a .pdf or .pptx file`},
		"unparseable pdf":     {engine.NewResource{Title: "Launch deck", Kind: engine.KindSlides, File: "deck.pdf"}, `cannot read deck.pdf as slides`},
		"malformed link":      {engine.NewResource{Title: "Preview deploy", Kind: engine.KindLink, Content: "preview.example.com"}, `resource "Preview deploy" has link "preview.example.com", which is not a full URL`},
		"javascript link":     {engine.NewResource{Title: "Preview deploy", Kind: engine.KindLink, Content: "javascript:alert(1)"}, `resource "Preview deploy" has a javascript: link`},
		"two links":           {engine.NewResource{Title: "Preview deploy", Kind: engine.KindLink, Content: "https://a.example https://b.example"}, `holds one URL`},
		"link as a file":      {engine.NewResource{Title: "Preview deploy", Kind: engine.KindLink, File: "notes.md"}, `takes its URL in content`},
		"unparseable xlsx":    {engine.NewResource{Title: "Order sheet", Kind: engine.KindSheet, File: "broken.xlsx"}, `cannot read broken.xlsx as a sheet`},
		"sheet from markdown": {engine.NewResource{Title: "Order sheet", Kind: engine.KindSheet, File: "notes.md"}, `attach a .csv, .tsv or .xlsx file`},
		"inline bad csv":      {engine.NewResource{Title: "Order sheet", Kind: engine.KindSheet, Content: "a,\"b\nc"}, `cannot read the content as a sheet`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.CreatePing(client.NewPing{Session: "s1", Folder: folder, Question: "Report written.", Finished: true, Resources: []engine.NewResource{tc.r}})
			refused(t, err, tc.says)
		})
	}
	if ps, _ := c.Pings(""); len(ps) != 0 {
		t.Fatalf("refused Pings were kept: %+v", ps)
	}
}
