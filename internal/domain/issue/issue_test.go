package issue_test

import (
	"reflect"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
)

func TestParseIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty", input: "", want: []string{}},
		{name: "whitespace only", input: "   ", want: []string{}},
		{name: "single", input: "CX-123", want: []string{"CX-123"}},
		{name: "multiple with spaces", input: " CX-1 , AB-22 ", want: []string{"CX-1", "AB-22"}},
		{name: "trailing comma ignored", input: "CX-1,", want: []string{"CX-1"}},
		{name: "lowercase project allowed", input: "cx-1", want: []string{"cx-1"}},
		{name: "single letter project rejected", input: "A-1", wantErr: true},
		{name: "missing number rejected", input: "CX-", wantErr: true},
		{name: "free text rejected", input: "not a key", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := issue.ParseIDs(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tc.input, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
