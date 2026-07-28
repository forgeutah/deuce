package workspace

import "testing"

func TestExecUserFromMetadata(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{
			// The shape DevPod actually produces: an array of merged
			// devcontainer metadata entries, one of which carries remoteUser.
			name:  "remoteUser in a multi-entry array",
			label: `[{"id":"ghcr.io/devcontainers/features/common-utils:2"},{"id":"ghcr.io/devcontainers/features/git:1"},{"remoteUser":"vscode"},{"entrypoint":"/usr/local/share/docker-init.sh"}]`,
			want:  "vscode",
		},
		{
			name:  "remoteUser wins over containerUser",
			label: `[{"containerUser":"node"},{"remoteUser":"vscode"}]`,
			want:  "vscode",
		},
		{
			name:  "containerUser is the fallback when no remoteUser is declared",
			label: `[{"containerUser":"node"}]`,
			want:  "node",
		},
		{
			// Metadata entries merge with later ones overriding earlier.
			name:  "later entries override earlier ones",
			label: `[{"remoteUser":"vscode"},{"remoteUser":"devuser"}]`,
			want:  "devuser",
		},
		{
			name:  "an empty override does not clobber an earlier value",
			label: `[{"remoteUser":"vscode"},{"remoteUser":""}]`,
			want:  "vscode",
		},
		{
			name:  "a bare object is accepted as well as an array",
			label: `{"remoteUser":"vscode"}`,
			want:  "vscode",
		},
		{
			name:  "numeric uid",
			label: `[{"remoteUser":"1000"}]`,
			want:  "1000",
		},
		{
			name:  "user:group form",
			label: `[{"remoteUser":"vscode:vscode"}]`,
			want:  "vscode:vscode",
		},
		{name: "no user declared", label: `[{"id":"some-feature"}]`, want: ""},
		{name: "empty array", label: `[]`, want: ""},
		{name: "empty label", label: ``, want: ""},
		{name: "malformed json", label: `[{"remoteUser":`, want: ""},
		{name: "wrong value type", label: `[{"remoteUser":123}]`, want: ""},
		{
			// The label is attacker-influenceable: a session member controls
			// the devcontainer.json in their own repo. The value lands in an
			// argv slot, so reject anything that isn't a plausible user spec
			// rather than trusting it.
			name:  "leading dash rejected so it cannot read as a flag",
			label: `[{"remoteUser":"--privileged"}]`,
			want:  "",
		},
		{name: "whitespace rejected", label: `[{"remoteUser":"vscode root"}]`, want: ""},
		{name: "shell metacharacters rejected", label: `[{"remoteUser":"a;b"}]`, want: ""},
		{name: "path traversal rejected", label: `[{"remoteUser":"../root"}]`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execUserFromMetadata(tt.label); got != tt.want {
				t.Errorf("execUserFromMetadata(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}
