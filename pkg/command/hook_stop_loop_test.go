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

func withStdin(t *testing.T, payload string) {
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
	prev := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = prev })
}

// graded reports whether the gate ran: WithEngine fires for every engine a command
// builds, and gradeQuietly cannot reach a verdict without one.
func runStopHookCapturing(t *testing.T, payload string) (stdout string, graded bool) {
	t.Helper()
	withStdin(t, payload)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prev }()

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

func stopPayload(t *testing.T, activeField string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "thing.rb"), []byte("class Thing\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"session_id":"s1","hook_event_name":"Stop","cwd":%q%s}`, dir, activeField)
}

func TestStopHook_StandsDownOnARepeatFire(t *testing.T) {
	stdout, graded := runStopHookCapturing(t, stopPayload(t, `,"stop_hook_active":true`))

	if graded {
		t.Error("the hook graded the repository on a repeat ask; it must stand down before doing any work")
	}
	if stdout != "" {
		t.Errorf("the hook spoke on a repeat ask, which is what continues the loop; stdout = %q", stdout)
	}
}

func TestStopHook_GradesOnAFirstAsk(t *testing.T) {
	for _, tt := range []struct {
		name        string
		activeField string
	}{
		{"field false", `,"stop_hook_active":false`},
		{"field absent", ``},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, graded := runStopHookCapturing(t, stopPayload(t, tt.activeField)); !graded {
				t.Error("the hook did not grade on a first ask; the loop guard has swallowed the feature")
			}
		})
	}
}

func TestReadHookInput_ReadsStopHookActive(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
		want    bool
	}{
		{"true", `{"cwd":"/tmp","stop_hook_active":true}`, true},
		{"false", `{"cwd":"/tmp","stop_hook_active":false}`, false},
		{"absent", `{"cwd":"/tmp"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withStdin(t, tt.payload)

			in, err := readHookInput()
			if err != nil {
				t.Fatal(err)
			}
			if in.StopHookActive != tt.want {
				t.Errorf("StopHookActive = %v, want %v (payload %s)", in.StopHookActive, tt.want, tt.payload)
			}
		})
	}
}
