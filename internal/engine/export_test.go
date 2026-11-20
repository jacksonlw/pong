package engine

import "testing"

// SetSofficeApp replaces where the Engine looks for LibreOffice outside PATH
// for the rest of t.
func SetSofficeApp(t *testing.T, path string) {
	old := sofficeApp
	sofficeApp = path
	t.Cleanup(func() { sofficeApp = old })
}
