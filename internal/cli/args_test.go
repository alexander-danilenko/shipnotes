package cli

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantHash    string
		wantOutput  string
		wantRepo    string
		wantJQL     string
		wantVersion bool
		wantErr     bool
	}{
		{name: "hash only", args: []string{"abc1234"}, wantHash: "abc1234", wantOutput: "SHIPNOTES.md"},
		{name: "flags before hash", args: []string{"-o", "out.md", "abc1234"}, wantHash: "abc1234", wantOutput: "out.md"},
		{name: "flags after hash", args: []string{"abc1234", "-o", "out.md"}, wantHash: "abc1234", wantOutput: "out.md"},
		{name: "long output flag", args: []string{"--output", "x.md", "HEAD"}, wantHash: "HEAD", wantOutput: "x.md"},
		{name: "repo-dir flag", args: []string{"HEAD", "--repo-dir", "/tmp/repo"}, wantHash: "HEAD", wantOutput: "SHIPNOTES.md", wantRepo: "/tmp/repo"},
		{name: "jql provided", args: []string{"HEAD", "--jql", "key IN (CX-1)"}, wantHash: "HEAD", wantOutput: "SHIPNOTES.md", wantJQL: "key IN (CX-1)"},
		{name: "terminator before hash", args: []string{"-o", "x.md", "--", "abc1234"}, wantHash: "abc1234", wantOutput: "x.md"},
		{name: "version long flag, no hash needed", args: []string{"--version"}, wantOutput: "SHIPNOTES.md", wantVersion: true},
		{name: "version short flag", args: []string{"-v"}, wantOutput: "SHIPNOTES.md", wantVersion: true},
		{name: "missing hash", args: []string{"-o", "out.md"}, wantErr: true},
		{name: "extra positional", args: []string{"a", "b"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", opts)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if opts.commitHash != tc.wantHash {
				t.Errorf("commitHash: got %q, want %q", opts.commitHash, tc.wantHash)
			}

			if opts.output != tc.wantOutput {
				t.Errorf("output: got %q, want %q", opts.output, tc.wantOutput)
			}

			if opts.repoDir != tc.wantRepo {
				t.Errorf("repoDir: got %q, want %q", opts.repoDir, tc.wantRepo)
			}

			if opts.showVersion != tc.wantVersion {
				t.Errorf("showVersion: got %v, want %v", opts.showVersion, tc.wantVersion)
			}

			if opts.jql != tc.wantJQL {
				t.Errorf("jql: got %q, want %q", opts.jql, tc.wantJQL)
			}
		})
	}
}

func TestSplitAtTerminator(t *testing.T) {
	before, after := splitAtTerminator([]string{"-o", "x", "--", "a", "b"})
	if !reflect.DeepEqual(before, []string{"-o", "x"}) || !reflect.DeepEqual(after, []string{"a", "b"}) {
		t.Errorf("got before=%v after=%v", before, after)
	}

	before, after = splitAtTerminator([]string{"HEAD", "-o", "x"})
	if !reflect.DeepEqual(before, []string{"HEAD", "-o", "x"}) || after != nil {
		t.Errorf("without terminator, expected all-before/nil-after, got before=%v after=%v", before, after)
	}
}
