// Package markdown renders the shipnotes model to Markdown. It is the
// adapter that implements the domain's notes.Renderer port, using Go's built-in
// text/template so the project stays dependency-free and we keep precise control
// over the exact bytes of the output (locked in place by the golden-file tests).
package markdown

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

// templateText is the Markdown template, compiled into the binary at build time.
//
//go:embed templates/shipnotes.tmpl
var templateText string

// releaseNotesTemplate is parsed once at startup. A parse error here is a bug
// in the template we ship, so panicking is the right response.
var releaseNotesTemplate = template.Must(
	template.New("shipnotes").Parse(templateText),
)

// Renderer turns the shipnotes model into Markdown. It satisfies
// notes.Renderer.
type Renderer struct{}

// New returns a Renderer.
func New() *Renderer {
	return &Renderer{}
}

// Render turns the data model into the final Markdown document.
func (*Renderer) Render(data notes.ReleaseNotes) (string, error) {
	var out strings.Builder
	if err := releaseNotesTemplate.Execute(&out, data); err != nil {
		return "", err
	}

	return out.String(), nil
}
