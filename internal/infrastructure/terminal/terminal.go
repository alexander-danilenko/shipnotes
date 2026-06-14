// Package terminal prints friendly, colorized status messages to the screen. It
// is the adapter that implements the domain's report.Reporter port, plus a
// couple of extra, CLI-only flourishes (the header and plain lines) used by the
// interface layer.
//
// It pulls in no external dependency: colors are plain ANSI escape codes (the
// same ones every terminal understands), and we simply skip them when the
// output is not an interactive terminal (for example when piped to a file).
package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI escape codes for the handful of colors we use. Wrapping text in one of
// these and the reset code is all "coloring" actually means.
const (
	codeReset  = "\033[0m"
	codeCyan   = "\033[36m"
	codeGreen  = "\033[32m"
	codeRed    = "\033[31m"
	codeYellow = "\033[33m"
	codeDim    = "\033[2m"
)

// Console writes status messages. Create one with New and pass it to the
// services that need to talk to the user (dependency injection). It satisfies
// the report.Reporter port.
type Console struct {
	out     io.Writer
	colored bool
}

// New builds a Console that writes to out. Colors are enabled only when out is
// a real terminal, so redirected or piped output stays clean and parseable.
func New(out io.Writer) *Console {
	return &Console{out: out, colored: isTerminal(out)}
}

// separatorWidth is the number of dashes in the header's divider line.
const separatorWidth = 32

// Header prints the title shown when the program starts.
func (c *Console) Header() {
	c.line(codeCyan, "🚀 Release Notes Generator")
	c.line(codeDim, strings.Repeat("━", separatorWidth))
}

// Status prints a "work in progress" line (for example "Validating commit...").
// We keep it simple and print a single line rather than an animated spinner,
// because the real output is the generated Markdown file.
func (c *Console) Status(message string) {
	c.line(codeCyan, "⏳ "+message)
}

// Success prints a green line, used for "✓ ..." confirmations.
func (c *Console) Success(message string) {
	c.line(codeGreen, message)
}

// Failure prints a red line, used for "✗ ..." results.
func (c *Console) Failure(message string) {
	c.line(codeRed, message)
}

// Warn prints a yellow warning line.
func (c *Console) Warn(message string) {
	c.line(codeYellow, message)
}

// Dim prints a muted line, used for secondary details.
func (c *Console) Dim(message string) {
	c.line(codeDim, message)
}

// Plain prints a line with no color at all.
func (c *Console) Plain(message string) {
	fmt.Fprintln(c.out, message)
}

// line writes message wrapped in the given color code (or plain when colors are
// disabled) followed by a newline.
func (c *Console) line(color, message string) {
	if c.colored {
		fmt.Fprintf(c.out, "%s%s%s\n", color, message, codeReset)

		return
	}

	fmt.Fprintln(c.out, message)
}

// isTerminal reports whether w is an interactive terminal. We only know how to
// answer this for real *os.File handles (stdout/stderr); anything else (a test
// buffer, a pipe) is treated as "not a terminal" so we omit color codes.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
