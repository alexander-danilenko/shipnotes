package git

import "testing"

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		host   string
		org    string
		repo   string
		isSSH  bool
		wantOK bool
	}{
		{
			name: "https with .git suffix",
			raw:  "https://github.com/acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: false, wantOK: true,
		},
		{
			name: "https without .git suffix",
			raw:  "https://github.com/acme/widgets",
			host: "github.com", org: "acme", repo: "widgets", isSSH: false, wantOK: true,
		},
		{
			name: "https with embedded user",
			raw:  "https://user@github.com/acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: false, wantOK: true,
		},
		{
			name: "scp-like ssh shorthand",
			raw:  "git@github.com:acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: true, wantOK: true,
		},
		{
			name: "ssh url",
			raw:  "ssh://git@github.com/acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: true, wantOK: true,
		},
		{
			name: "ssh url with port",
			raw:  "ssh://git@github.com:22/acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: true, wantOK: true,
		},
		{
			name: "custom ssh host alias",
			raw:  "git@github-work:acme/widgets.git",
			host: "github-work", org: "acme", repo: "widgets", isSSH: true, wantOK: true,
		},
		{
			name: "git protocol",
			raw:  "git://github.com/acme/widgets.git",
			host: "github.com", org: "acme", repo: "widgets", isSSH: false, wantOK: true,
		},
		{
			name: "nested groups keep first and last segment",
			raw:  "git@gitlab.com:group/sub/widgets.git",
			host: "gitlab.com", org: "group", repo: "widgets", isSSH: true, wantOK: true,
		},
		{
			name:   "missing repo segment",
			raw:    "https://github.com/acme",
			wantOK: false,
		},
		{
			name:   "not a url",
			raw:    "not a remote",
			wantOK: false,
		},
		{
			name:   "host smuggling an ssh flag is rejected",
			raw:    "git@-oProxyCommand=evil:acme/widgets.git",
			wantOK: false,
		},
		{
			name:   "empty",
			raw:    "",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, org, repo, isSSH, ok := parseRemoteURL(tc.raw)

			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}

			if !tc.wantOK {
				return
			}

			if host != tc.host || org != tc.org || repo != tc.repo || isSSH != tc.isSSH {
				t.Errorf("got host=%q org=%q repo=%q ssh=%v; want host=%q org=%q repo=%q ssh=%v",
					host, org, repo, isSSH, tc.host, tc.org, tc.repo, tc.isSSH)
			}
		})
	}
}
