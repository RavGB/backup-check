package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// execute runs the real Cobra command with the given arguments and
// returns what it printed and the error ExecuteC produced, so the
// tests exercise the same exit plumbing as main.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
	return buf.String(), err
}

func TestCommandIdenticalTreesExit0(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "a.txt"), "hello", baseTime)
	writeSized(t, filepath.Join(bak, "a.txt"), "hello", baseTime)

	out, err := execute(t, "--source", src, "--backup", bak)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(out, "Verdict: Backup is current") {
		t.Errorf("output missing clean verdict:\n%s", out)
	}
}

func TestCommandProblemsExit1(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "gone.txt"), "data", baseTime)

	out, err := execute(t, "--source", src, "--backup", bak)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("err = %v, want exitError code 1", err)
	}
	if exitCodeForError(err) != 1 {
		t.Errorf("exitCodeForError = %d, want 1", exitCodeForError(err))
	}
	if !strings.Contains(out, "Missing") || !strings.Contains(out, "gone.txt") {
		t.Errorf("report missing the missing file:\n%s", out)
	}
}

func TestCommandMissingFlagIsUsageError(t *testing.T) {
	_, err := execute(t, "--source", "/tmp")
	if err == nil {
		t.Fatal("expected an error for a missing required flag")
	}
	if !isUsageError(err) || exitCodeForError(err) != 2 {
		t.Errorf("err = %v: usage=%v code=%d, want usage/2", err, isUsageError(err), exitCodeForError(err))
	}
}

func TestCommandBadDirectoryIsUsageError(t *testing.T) {
	src, _ := pair(t)
	_, err := execute(t, "--source", src, "--backup", filepath.Join(src, "does-not-exist"))
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want usageError", err)
	}
	if exitCodeForError(err) != 2 {
		t.Errorf("exit code = %d, want 2", exitCodeForError(err))
	}
}

func TestCommandUnknownFlagIsUsageError(t *testing.T) {
	_, err := execute(t, "--nope")
	if err == nil || !isUsageError(err) || exitCodeForError(err) != 2 {
		t.Errorf("err = %v, want a usage error with code 2", err)
	}
}

func TestCommandToleranceFlagParses(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "f.txt"), "same", baseTime)
	// Backup copy 30 seconds older: fine with a 1m tolerance.
	writeSized(t, filepath.Join(bak, "f.txt"), "same", baseTime.Add(-30*time.Second))
	_, err := execute(t, "--source", src, "--backup", bak, "--tolerance", "1m")
	if err != nil {
		t.Fatalf("err = %v, want nil with --tolerance 1m", err)
	}
	// And a bad duration is a usage error.
	_, err = execute(t, "--source", src, "--backup", bak, "--tolerance", "soon")
	if err == nil || exitCodeForError(err) != 2 {
		t.Errorf("bad duration: err = %v, want usage code 2", err)
	}
}

func TestCommandJSONOutput(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "a.txt"), "hello", baseTime)
	writeSized(t, filepath.Join(bak, "a.txt"), "hello", baseTime)
	out, err := execute(t, "--source", src, "--backup", bak, "--json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, `"verdict": "Backup is current"`) {
		t.Errorf("JSON output missing verdict:\n%s", out)
	}
}
