package engine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Resource kinds.
const (
	KindImage    = "image"
	KindDocument = "document"
	KindSheet    = "sheet"
	KindSlides   = "slides"
	KindLink     = "link"
	KindChange   = "change"
	KindError    = "error"
	KindOutput   = "output"
	KindFile     = "file"
	KindText     = "text"
)

var resourceKinds = []string{KindImage, KindDocument, KindSheet, KindSlides, KindLink, KindChange, KindError, KindOutput, KindFile, KindText}

// The content types of Resources the Engine converts (convert.go).
const (
	LinkContentType  = "text/uri-list"
	SheetContentType = "application/vnd.pong.sheet+json"
	PDFContentType   = "application/pdf"
	PPTXContentType  = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

// Resource is something attached to a Ping. Its content is the Engine's own
// copy, read with GET /pings/{id}/resources/{resource id}.
type Resource struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	File        string `json:"file,omitempty"` // the file it was copied from, when attached by file
	ContentType string `json:"content_type"`
}

// NewResource is one Resource in a NewPing: exactly one of Content (inline
// text) or File (a location the Engine copies at send time).
type NewResource struct {
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Content string `json:"content,omitempty"`
	File    string `json:"file,omitempty"`
}

// DefaultRetention is how long a Ping that is no longer waiting, and its
// Resources, are kept.
const DefaultRetention = 30 * 24 * time.Hour

// DefaultSweepEvery is how often the Engine removes Pings past retention.
const DefaultSweepEvery = time.Hour

func (s *pings) resourceDir() string { return filepath.Join(s.dir, "resources") }

// validateResources returns what to fix in rs, or "".
func validateResources(rs []NewResource) string {
	for i, r := range rs {
		if w := len(strings.Fields(r.Title)); w < 2 || w > 4 {
			return fmt.Sprintf("resource %d's title %q has %d words: give each Resource a title of two to four words", i+1, r.Title, w)
		}
		known := false
		for _, k := range resourceKinds {
			known = known || r.Kind == k
		}
		if !known {
			return fmt.Sprintf("resource %q has kind %q: use one of %s", r.Title, r.Kind, strings.Join(resourceKinds, ", "))
		}
		if (r.File == "") == (r.Content == "") {
			return fmt.Sprintf("resource %q needs exactly one of file (a location) or content (inline text)", r.Title)
		}
		if msg := r.shapeProblem(); msg != "" {
			return msg
		}
	}
	return ""
}

// shapeProblem returns what to fix in a link, sheet or slides Resource that
// can be told without reading its file, or "".
func (r NewResource) shapeProblem() string {
	ext := strings.ToLower(filepath.Ext(r.File))
	switch r.Kind {
	case KindLink:
		if r.File != "" {
			return fmt.Sprintf("resource %q is a link, which takes its URL in content, not file: put the URL in content", r.Title)
		}
		link := strings.TrimSpace(r.Content)
		if len(strings.Fields(link)) != 1 {
			return fmt.Sprintf("resource %q is a link, which holds one URL: send one link Resource for each URL", r.Title)
		}
		switch fault := linkFault(link); fault {
		case "":
		case notFullURL:
			return fmt.Sprintf("resource %q has link %q, which is not a full URL: give a link with a scheme, such as https://", r.Title, link)
		default:
			return fmt.Sprintf("resource %q has a %s: link, which Pong cannot open: use another link", r.Title, fault)
		}
	case KindSheet:
		if r.File != "" && ext != ".csv" && ext != ".tsv" && ext != ".xlsx" {
			return fmt.Sprintf("resource %q is a sheet from %s: attach a .csv, .tsv or .xlsx file, or comma-separated content", r.Title, r.File)
		}
	case KindSlides:
		if r.File == "" {
			return fmt.Sprintf("resource %q is slides, which need a file: attach a .pdf or .pptx as file, or send the text as a document", r.Title)
		}
		if ext != ".pdf" && ext != ".pptx" {
			return fmt.Sprintf("resource %q is slides from %s: attach a .pdf or .pptx file, or use kind file", r.Title, r.File)
		}
	}
	return ""
}

// storeResources copies each Resource's content into the data directory.
// A relative file is taken from folder. It returns a badRequest naming a file
// it cannot read.
func (s *pings) storeResources(rs []NewResource, folder string) ([]Resource, error) {
	var out []Resource
	fail := func(err error) ([]Resource, error) {
		s.removeResources(out)
		return nil, err
	}
	if len(rs) > 0 {
		if err := os.MkdirAll(s.resourceDir(), 0o700); err != nil {
			return nil, err
		}
	}
	for _, r := range rs {
		data := []byte(r.Content)
		ctype := "text/plain; charset=utf-8"
		path := ""
		if r.File != "" {
			if path = r.File; !filepath.IsAbs(path) {
				path = filepath.Join(folder, path)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return fail(badRequest(fmt.Sprintf("cannot read resource file %s: %v", r.File, unwrapPathErr(err))))
			}
			data = b
			if ctype = mime.TypeByExtension(filepath.Ext(path)); ctype == "" {
				ctype = http.DetectContentType(b)
			}
		}
		kind, data, ctype, err := convert(r, path, data, ctype)
		if err != nil {
			return fail(err)
		}
		buf := make([]byte, 8)
		if _, err := rand.Read(buf); err != nil {
			return fail(err)
		}
		res := Resource{ID: hex.EncodeToString(buf), Title: r.Title, Kind: kind, File: r.File, ContentType: ctype}
		if err := os.WriteFile(filepath.Join(s.resourceDir(), res.ID), data, 0o600); err != nil {
			return fail(err)
		}
		out = append(out, res)
	}
	return out, nil
}

func unwrapPathErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

func (s *pings) removeResources(rs []Resource) {
	for _, r := range rs {
		os.Remove(filepath.Join(s.resourceDir(), r.ID))
	}
}

func (s *pings) serveResource(w http.ResponseWriter, r *http.Request) {
	if s.set != nil && s.set.has(r.PathValue("id")) {
		// Another computer's: fetched from it now, when the human opens it.
		f, err := s.set.fetchResource(r.PathValue("id"), r.PathValue("rid"))
		var re errResource
		switch {
		case errors.As(err, &re):
			writeError(w, http.StatusBadGateway, err.Error())
		case err != nil:
			writeError(w, http.StatusServiceUnavailable, err.Error()+": its Resources open once it is back")
		default:
			w.Header().Set("Content-Type", f.mime)
			w.Header().Set("X-Pong-Title", f.title)
			w.Header().Set("X-Pong-Kind", f.kind)
			w.WriteHeader(http.StatusOK)
			w.Write(f.data)
		}
		return
	}
	p, err := s.get(r.PathValue("id"))
	if err != nil {
		writePing(w, p, err)
		return
	}
	for _, res := range p.Resources {
		if res.ID != r.PathValue("rid") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.resourceDir(), res.ID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", res.ContentType)
		w.Header().Set("X-Pong-Title", res.Title)
		w.Header().Set("X-Pong-Kind", res.Kind)
		w.WriteHeader(http.StatusOK)
		w.Write(b)
		return
	}
	writeError(w, http.StatusNotFound, "this Ping has no such Resource: check the id")
}

// sweep removes Pings that left waiting longer than retention ago, with their
// Resources and their events.
func (s *pings) sweep(retention time.Duration) error {
	ps, err := s.list("")
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-retention)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range ps {
		if p.State == StateWaiting || p.UpdatedAt.After(cutoff) {
			continue
		}
		if _, err := s.db.Exec(`DELETE FROM pings WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DELETE FROM events WHERE kind LIKE 'ping.%' AND json_extract(data, '$.id') = ?`, p.ID); err != nil {
			return err
		}
		s.removeResources(p.Resources)
	}
	return nil
}

// sweepEvery sweeps now and then every interval until the Engine stops.
func (s *pings) sweepEvery(retention, every time.Duration) {
	s.sweep(retention)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.sweep(retention)
		case <-s.closing:
			return
		}
	}
}

// createWithResources creates the Ping for a validated n, first copying its
// Resources into the data directory.
func (s *pings) createWithResources(n NewPing) (Ping, error) {
	res, err := s.storeResources(n.Resources, n.Folder)
	if err != nil {
		return Ping{}, err
	}
	p := n.ping()
	p.Resources = res
	p, err = s.create(p)
	if err != nil {
		s.removeResources(res)
	}
	return p, err
}
