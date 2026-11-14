package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// writeSized writes a file with the given content and modtime.
func writeSized(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// pair makes an empty source/backup pair of temp directories.
func pair(t *testing.T) (src, bak string) {
	t.Helper()
	root := t.TempDir()
	src = filepath.Join(root, "source")
	bak = filepath.Join(root, "backup")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bak, 0o755); err != nil {
		t.Fatal(err)
	}
	return src, bak
}

func defaultOpts(src, bak string) *options {
	return &options{source: src, backup: bak, workers: 2, tolerance: 2 * time.Second, maxList: 50}
}

func check(t *testing.T, opts *options) *Result {
	t.Helper()
	res, err := checkBackup(opts)
	if err != nil {
		t.Fatalf("checkBackup: %v", err)
	}
	return res
}

func TestIdenticalTreesHaveNoProblems(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "a.txt"), "hello", baseTime)
	writeSized(t, filepath.Join(bak, "a.txt"), "hello", baseTime)
	writeSized(t, filepath.Join(src, "sub", "b.bin"), "0123456789", baseTime)
	writeSized(t, filepath.Join(bak, "sub", "b.bin"), "0123456789", baseTime)

	res := check(t, defaultOpts(src, bak))
	if res.ProblemCount() != 0 {
		t.Errorf("problems = %d, want 0 (%+v)", res.ProblemCount(), res)
	}
	if res.SourceFiles != 2 || res.BackupFiles != 2 {
		t.Errorf("files = %d/%d, want 2/2", res.SourceFiles, res.BackupFiles)
	}
	res.finalize()
	if res.Verdict != "Backup is current" || exitCodeForResult(res) != 0 {
		t.Errorf("verdict = %q, want current/0", res.Verdict)
	}
}

func TestMissingFile(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "gone.txt"), "data", baseTime)

	res := check(t, defaultOpts(src, bak))
	if len(res.Missing) != 1 || res.Missing[0].Path != "gone.txt" {
		t.Fatalf("Missing = %+v, want gone.txt", res.Missing)
	}
	if res.ProblemCount() != 1 {
		t.Errorf("problems = %d, want 1", res.ProblemCount())
	}
}

func TestOutdatedBeyondAndWithinTolerance(t *testing.T) {
	src, bak := pair(t)
	// Source 10s newer than backup: outside the 2s tolerance.
	writeSized(t, filepath.Join(src, "old.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "old.txt"), "same", baseTime.Add(-10*time.Second))
	// Source 1s newer: inside tolerance (timestamp rounding).
	writeSized(t, filepath.Join(src, "fresh.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "fresh.txt"), "same", baseTime.Add(-1*time.Second))
	// Backup newer than source: fine, never outdated.
	writeSized(t, filepath.Join(src, "ahead.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "ahead.txt"), "same", baseTime.Add(time.Hour))

	res := check(t, defaultOpts(src, bak))
	if len(res.Outdated) != 1 || res.Outdated[0].Path != "old.txt" {
		t.Fatalf("Outdated = %+v, want only old.txt", res.Outdated)
	}
	if res.ProblemCount() != 1 {
		t.Errorf("problems = %d, want 1", res.ProblemCount())
	}
}

func TestSizeDiffers(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "f.txt"), "longer-content", baseTime)
	writeSized(t, filepath.Join(bak, "f.txt"), "short", baseTime)

	res := check(t, defaultOpts(src, bak))
	if len(res.SizeDiffers) != 1 || res.ProblemCount() != 1 {
		t.Fatalf("SizeDiffers = %+v, problems = %d, want 1/1", res.SizeDiffers, res.ProblemCount())
	}
	if res.SizeDiffers[0].SourceSize != int64(len("longer-content")) {
		t.Errorf("SourceSize = %d", res.SizeDiffers[0].SourceSize)
	}
}

// Same size, different bytes: only --hash can see it.
func TestContentDiffersOnlyWithHash(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "f.txt"), "aaaa", baseTime)
	writeSized(t, filepath.Join(bak, "f.txt"), "bbbb", baseTime)

	noHash := check(t, defaultOpts(src, bak))
	if noHash.ProblemCount() != 0 {
		t.Errorf("without --hash: problems = %d, want 0", noHash.ProblemCount())
	}

	opts := defaultOpts(src, bak)
	opts.hash = true
	withHash := check(t, opts)
	if len(withHash.ContentDiffers) != 1 || withHash.ProblemCount() != 1 {
		t.Fatalf("with --hash: ContentDiffers = %+v, problems = %d, want 1/1",
			withHash.ContentDiffers, withHash.ProblemCount())
	}
}

func TestIdenticalContentPassesHash(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "big.bin"), strings.Repeat("x", 100000), baseTime)
	writeSized(t, filepath.Join(bak, "big.bin"), strings.Repeat("x", 100000), baseTime)

	opts := defaultOpts(src, bak)
	opts.hash = true
	res := check(t, opts)
	if res.ProblemCount() != 0 {
		t.Errorf("problems = %d, want 0", res.ProblemCount())
	}
}

func TestExtrasAreNotProblems(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "a.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "a.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "kept-from-before.txt"), "old stuff", baseTime)

	res := check(t, defaultOpts(src, bak))
	if res.ProblemCount() != 0 {
		t.Errorf("problems = %d, want 0 (extras are information)", res.ProblemCount())
	}
	if len(res.Extras) != 1 || res.Extras[0].Path != "kept-from-before.txt" {
		t.Fatalf("Extras = %+v", res.Extras)
	}
	if res.ExtrasBytes != int64(len("old stuff")) {
		t.Errorf("ExtrasBytes = %d", res.ExtrasBytes)
	}
}

func TestEmptySourceTree(t *testing.T) {
	src, bak := pair(t)
	res := check(t, defaultOpts(src, bak))
	if res.ProblemCount() != 0 || res.SourceFiles != 0 {
		t.Errorf("empty source: problems = %d, files = %d, want 0/0", res.ProblemCount(), res.SourceFiles)
	}
}

func TestSymlinksComparedByTarget(t *testing.T) {
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "real.txt"), "same", baseTime)
	writeSized(t, filepath.Join(bak, "real.txt"), "same", baseTime)
	if err := os.Symlink("real.txt", filepath.Join(src, "good-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(bak, "good-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "moved-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.txt", filepath.Join(bak, "moved-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "lost-link")); err != nil {
		t.Fatal(err)
	}

	res := check(t, defaultOpts(src, bak))
	if len(res.LinkDiffers) != 1 || res.LinkDiffers[0].Path != "moved-link" {
		t.Errorf("LinkDiffers = %+v, want only moved-link", res.LinkDiffers)
	}
	if len(res.Missing) != 1 || res.Missing[0].Path != "lost-link" {
		t.Errorf("Missing = %+v, want only lost-link", res.Missing)
	}
	if res.SourceLinks != 3 || res.BackupLinks != 2 {
		t.Errorf("links = %d/%d, want 3/2", res.SourceLinks, res.BackupLinks)
	}
	// A symlinked directory must not be descended into: its target's
	// files are not source entries.
	if err := os.Symlink(filepath.Join(src), filepath.Join(bak, "dir-link")); err != nil {
		t.Fatal(err)
	}
	res2 := check(t, defaultOpts(src, bak))
	for _, e := range res2.Extras {
		if strings.HasPrefix(e.Path, "dir-link/") {
			t.Errorf("walk descended into a symlinked directory: extra %s", e.Path)
		}
	}
}

// Unreadable entries count as problems (documented rule). Skipped
// when running as root, because root can read anything.
func TestUnreadableCountsAsProblem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	src, bak := pair(t)
	writeSized(t, filepath.Join(src, "secret.txt"), "aaaa", baseTime)
	writeSized(t, filepath.Join(bak, "secret.txt"), "bbbb", baseTime)
	if err := os.Chmod(filepath.Join(bak, "secret.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(bak, "secret.txt"), 0o644) })

	opts := defaultOpts(src, bak)
	opts.hash = true // hashing is what needs to read the file
	res := check(t, opts)
	if len(res.Unreadable) != 1 || res.ProblemCount() != 1 {
		t.Fatalf("Unreadable = %+v, problems = %d, want 1/1", res.Unreadable, res.ProblemCount())
	}
}

func TestVerdictAndExitMapping(t *testing.T) {
	ok := &Result{}
	ok.finalize()
	if ok.Verdict != "Backup is current" || ok.Problems != 0 {
		t.Errorf("clean verdict = %q/%d", ok.Verdict, ok.Problems)
	}
	bad := &Result{Missing: []Problem{{Path: "x"}}, Outdated: []Problem{{Path: "y"}}}
	bad.finalize()
	if bad.Verdict != "Backup has 2 problems" || bad.Problems != 2 {
		t.Errorf("bad verdict = %q/%d", bad.Verdict, bad.Problems)
	}
	if got := exitCodeForError(&exitError{code: 1}); got != 1 {
		t.Errorf("exitError maps to %d, want 1", got)
	}
	if got := exitCodeForError(&usageError{msg: "x"}); got != 2 {
		t.Errorf("usageError maps to %d, want 2", got)
	}
	if got := exitCodeForError(&runtimeError{msg: "x"}); got != 1 {
		t.Errorf("runtimeError maps to %d, want 1", got)
	}
}

// exitCodeForResult mirrors the RunE decision for tests.
func exitCodeForResult(r *Result) int {
	if r.ProblemCount() > 0 {
		return 1
	}
	return 0
}
