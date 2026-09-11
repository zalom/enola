package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/pkg/bootstrap"
)

// runStopHook drives the stop hook the way the harness does — payload on stdin, verdict
// on stdout — and reports whether it tried to GRADE.
//
// "Tried to grade" is the assertion a loop guard needs, and silence alone cannot carry
// it: a hook that grades a repository with no baseline is also silent, so a test that
// only checked stdout would pass against the very bug it is meant to catch. WithEngine
// runs for every engine a command builds and gradeQuietly cannot reach a verdict without
// one, so the spy firing is exactly "the gate ran".
func runStopHookCapturing(t *testing.T, payload string) (stdout string, graded bool) {
	t.Helper()

	stdin, err := os.CreateTemp(t.TempDir(), "payload-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	prevIn := os.Stdin
	os.Stdin = stdin
	defer func() { os.Stdin = prevIn }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevOut := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prevOut }()

	testRunner().
		WithEngine(func(*bootstrap.Engine) { graded = true }).
		runStopHook(context.Background())

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), graded
}

// repoWithSource is a directory the engine will accept as a repository to snapshot.
func repoWithSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "thing.rb"), []byte("class Thing\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestStopHook_StandsDownOnARepeatFire is the loop guard.
//
// A Stop hook that hands the agent context is asked again once the agent has acted on
// it, and the second ask carries stop_hook_active. Nothing about the repository changed
// between the two asks, so re-grading reaches the same verdict and says it again, and the
// session cannot end until the harness's block cap trips. The only way out is for the
// hook to recognise the repeat and stand down.
func TestStopHook_StandsDownOnARepeatFire(t *testing.T) {
	payload := fmt.Sprintf(
		`{"session_id":"s1","hook_event_name":"Stop","cwd":%q,"stop_hook_active":true}`,
		repoWithSource(t),
	)

	stdout, graded := runStopHookCapturing(t, payload)

	if graded {
		t.Error("the hook graded the repository on a repeat fire; it must stand down before doing any work")
	}
	if stdout != "" {
		t.Errorf("the hook spoke on a repeat fire, which is what continues the loop; stdout = %q", stdout)
	}
}

// TestStopHook_GradesOnAFirstFire keeps the guard narrow. Standing down whenever the
// field is absent or false would turn the fix into "the hook never runs", which passes
// the test above while removing the feature.
func TestStopHook_GradesOnAFirstFire(t *testing.T) {
	for _, tt := range []struct {
		name  string
		field string
	}{
		{"field false", `,"stop_hook_active":false`},
		{"field absent", ``},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := fmt.Sprintf(
				`{"session_id":"s1","hook_event_name":"Stop","cwd":%q%s}`,
				repoWithSource(t), tt.field,
			)

			if _, graded := runStopHookCapturing(t, payload); !graded {
				t.Error("the hook did not grade on a first fire; the loop guard has swallowed the feature")
			}
		})
	}
}
