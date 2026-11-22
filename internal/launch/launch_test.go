package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestViewArgv(t *testing.T) {
	for _, c := range []struct {
		t    Target
		want []string
	}{
		{Target{Path: "a.go", Line: 12}, []string{"rig", "view", "--line", "12", "--", "a.go"}},
		{Target{Path: "a.go"}, []string{"rig", "view", "--", "a.go"}},
		{Target{Path: "-odd.go", Diff: true}, []string{"rig", "view", "--diff", "--", "-odd.go"}},
		{Target{Path: "a.go", Commit: "abc123"}, []string{"rig", "view", "--commit", "abc123", "--", "a.go"}},
	} {
		if got := ViewArgv("rig", c.t); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %q", c.t, got)
		}
	}
}

func TestEditorAtLine(t *testing.T) {
	for editor, wantLine := range map[string]bool{"nvim": true, "/usr/bin/vim -p": true, "code --wait": false, "hx": false} {
		t.Setenv("VISUAL", editor)
		argv := Argv(Target{Root: "/tmp", Path: "a b.go", Line: 12})
		got := strings.Contains(argv[2], `"+$2"`) && argv[len(argv)-1] == "12"
		if got != wantLine {
			t.Errorf("%s: argv = %q", editor, argv)
		}
	}
	// The line and the path arrive as separate arguments, however the path
	// is spelled.
	vi := filepath.Join(t.TempDir(), "vi")
	if err := os.WriteFile(vi, []byte("#!/bin/sh\nprintf '%s|' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", vi)
	argv := Argv(Target{Root: t.TempDir(), Path: "a b.go", Line: 3})
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil || string(out) != "+3|a b.go|" {
		t.Fatalf("ran %q: %q, %v", argv, out, err)
	}
}
