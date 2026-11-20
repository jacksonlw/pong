package engine

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// sofficeApp is where LibreOffice keeps soffice on a Mac, looked at when
// soffice is not on PATH.
var sofficeApp = "/Applications/LibreOffice.app/Contents/MacOS/soffice"

// pdfMagic is how every PDF file starts.
var pdfMagic = []byte{'%', 'P', 'D', 'F', '-'}

// sofficeTimeout is how long a pptx may take to become a PDF.
const sofficeTimeout = 60 * time.Second

// convert turns a validated Resource's content into what the Engine stores:
// a link as text/uri-list, a sheet as sheet JSON and slides as a PDF. path is
// the file it was read from, or "" for inline content. A pptx that cannot be
// converted is kept as kind file. Other kinds are returned as they are.
func convert(r NewResource, path string, data []byte, ctype string) (kind string, out []byte, outType string, err error) {
	switch r.Kind {
	case KindLink:
		return KindLink, []byte(strings.TrimSpace(r.Content)), LinkContentType, nil
	case KindSheet:
		s, err := readSheet(r.Title, path, data)
		if err != nil {
			what := "the content"
			if path != "" {
				what = r.File
			}
			return "", nil, "", badRequest(fmt.Sprintf("resource %q: cannot read %s as a sheet (%v): attach a .csv, .tsv or .xlsx file, or comma-separated content", r.Title, what, err))
		}
		b, err := json.Marshal(s)
		return KindSheet, b, SheetContentType, err
	case KindSlides:
		if strings.EqualFold(filepath.Ext(path), ".pptx") {
			if pdf, ok := pptxToPDF(path); ok {
				return KindSlides, pdf, PDFContentType, nil
			}
			return KindFile, data, PPTXContentType, nil
		}
		if !bytes.HasPrefix(data, pdfMagic) {
			return "", nil, "", badRequest(fmt.Sprintf("resource %q: cannot read %s as slides, because its content is not a .pdf: attach a .pdf or .pptx file, or use kind file", r.Title, r.File))
		}
		return KindSlides, data, PDFContentType, nil
	}
	return r.Kind, data, ctype, nil
}

// sheetDoc is a sheet as the Engine stores it and the apps draw it.
type sheetDoc struct {
	Sheets []sheet `json:"sheets"`
}

type sheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

// readSheet reads a csv, tsv or xlsx file, or inline comma-separated text
// when path is "". A csv or tsv is one sheet named title; an xlsx gives one
// sheet per worksheet, with each cell's display value.
func readSheet(title, path string, data []byte) (sheetDoc, error) {
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return sheetDoc{}, err
		}
		defer f.Close()
		var doc sheetDoc
		for _, name := range f.GetSheetList() {
			rows, err := f.GetRows(name)
			if err != nil {
				return sheetDoc{}, err
			}
			if rows == nil {
				rows = [][]string{}
			}
			doc.Sheets = append(doc.Sheets, sheet{Name: name, Rows: rows})
		}
		return doc, nil
	}
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	if strings.EqualFold(filepath.Ext(path), ".tsv") {
		cr.Comma, cr.LazyQuotes = '\t', true
	}
	rows, err := cr.ReadAll()
	if err != nil {
		return sheetDoc{}, err
	}
	if rows == nil {
		rows = [][]string{}
	}
	return sheetDoc{Sheets: []sheet{{Name: title, Rows: rows}}}, nil
}

// pptxToPDF converts the pptx at path with LibreOffice's soffice, found on
// PATH or at sofficeApp. ok is false when there is no soffice, or it fails or
// takes longer than sofficeTimeout.
func pptxToPDF(path string) (pdf []byte, ok bool) {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		if _, err := os.Stat(sofficeApp); err != nil {
			return nil, false
		}
		soffice = sofficeApp
	}
	out, err := os.MkdirTemp("", "pong-slides-")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(out)
	ctx, cancel := context.WithTimeout(context.Background(), sofficeTimeout)
	defer cancel()
	// Its own profile, so a LibreOffice the human has open does not take the
	// conversion over and leave no PDF.
	cmd := exec.CommandContext(ctx, soffice, "-env:UserInstallation=file://"+filepath.ToSlash(filepath.Join(out, "profile")),
		"--headless", "--convert-to", "pdf", "--outdir", out, path)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(out, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".pdf"))
	if err != nil || !bytes.HasPrefix(b, pdfMagic) {
		return nil, false
	}
	return b, true
}
