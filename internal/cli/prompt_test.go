package cli_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/cli"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

func TestForIssueIDs(t *testing.T) {
	console := terminal.New(io.Discard)

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty input skips", input: "", want: []string{}},
		{name: "blank line skips", input: "\n", want: []string{}},
		{name: "valid list", input: "CX-1,CX-2\n", want: []string{"CX-1", "CX-2"}},
		{name: "re-asks past invalid line", input: "bad input\nCX-1\n", want: []string{"CX-1"}},
		{name: "invalid then EOF skips", input: "bad input", want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cli.NewPrompt(console, strings.NewReader(tc.input)).ForIssueIDs()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
