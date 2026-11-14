// backup-check verifies that a backup folder really matches the
// original: nothing missing, nothing out of date, nothing the wrong
// size, and (with --hash) nothing with different content.
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// usageError marks a problem with how the tool was invoked: a bad flag
// value, a missing directory, an unwritable --output path. Usage
// errors print the error plus the usage text and exit 2.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// runtimeError marks a failure after the invocation itself was fine
// (for example the report file could not be written). It prints
// plainly and exits 1.
type runtimeError struct{ msg string }

func (e *runtimeError) Error() string { return e.msg }

// exitError carries a process exit code with no message of its own:
// the report has already been printed, so main stays silent.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

type options struct {
	source    string
	backup    string
	hash      bool
	workers   int
	tolerance time.Duration
	maxList   int
	jsonOut   bool
	output    string
}

func newRootCmd() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "backup-check --source DIR --backup DIR",
		Short: "Verify that a backup folder really matches the original",
		Long: `backup-check compares an original folder with its backup, file by file,
and reports anything that is missing, out of date, the wrong size, or
(with --hash) different in content. Files that exist only in the backup
are listed as extras for information; they are never counted as
problems, because people keep older files in backups on purpose.

Files are classified with one rule each, in this order: missing,
different type or link target, size differs, outdated (the source copy
is newer than the backup copy by more than --tolerance), and content
differs (only with --hash, and only when size and time look fine).
Symlinks are never followed: a symlink is an entry of its own and is
compared by its target path. Entries that cannot be read are counted
as problems, because a backup that cannot be read cannot be trusted.
Empty directories are not compared; files and links are what count.

Exit codes: 0 = the backup is current, 1 = at least one problem,
2 = the command was used incorrectly.`,
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, &opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.source, "source", "", "original folder (required)")
	f.StringVar(&opts.backup, "backup", "", "backup folder to verify (required)")
	f.BoolVar(&opts.hash, "hash", false, "also compare file contents with SHA-256 (slower, certain)")
	f.IntVar(&opts.workers, "workers", 4, "files hashed in parallel when --hash is set")
	f.DurationVar(&opts.tolerance, "tolerance", 2*time.Second, "how much newer the source may be before a file counts as outdated")
	f.IntVar(&opts.maxList, "max-list", 50, "most entries shown per report section (0 shows counts only)")
	f.BoolVar(&opts.jsonOut, "json", false, "print the full result as JSON instead of text")
	f.StringVar(&opts.output, "output", "", "also write the text report to this file")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("backup")
	return cmd
}

func runCheck(cmd *cobra.Command, opts *options) error {
	if err := validateOptions(opts); err != nil {
		return err
	}

	// Open the report file before scanning so a bad path fails fast
	// as a usage error instead of after a long scan.
	var outFile *os.File
	if opts.output != "" {
		fh, err := os.Create(opts.output)
		if err != nil {
			return &usageError{msg: fmt.Sprintf("cannot write report to %s: %v", opts.output, err)}
		}
		defer fh.Close()
		outFile = fh
	}

	res, err := checkBackup(opts)
	if err != nil {
		return err
	}
	res.finalize()

	text := renderText(res, opts.maxList)
	if opts.jsonOut {
		data, err := marshalResult(res)
		if err != nil {
			return &runtimeError{msg: fmt.Sprintf("could not render JSON: %v", err)}
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		fmt.Fprint(cmd.OutOrStdout(), text)
	}
	if outFile != nil {
		if _, err := outFile.WriteString(text); err != nil {
			return &runtimeError{msg: fmt.Sprintf("could not write report to %s: %v", opts.output, err)}
		}
	}

	if res.Problems > 0 {
		return &exitError{code: 1}
	}
	return nil
}

// validateOptions checks everything that can be known before scanning:
// both folders exist and are directories, and the numeric flags are in
// range. Anything wrong here is a usage error.
func validateOptions(opts *options) error {
	for _, item := range []struct{ flag, path string }{
		{"--source", opts.source},
		{"--backup", opts.backup},
	} {
		info, err := os.Stat(item.path)
		if err != nil {
			return &usageError{msg: fmt.Sprintf("%s %s: %v", item.flag, item.path, err)}
		}
		if !info.IsDir() {
			return &usageError{msg: fmt.Sprintf("%s %s is not a directory", item.flag, item.path)}
		}
	}
	if opts.workers < 1 {
		return &usageError{msg: "--workers must be at least 1"}
	}
	if opts.maxList < 0 {
		return &usageError{msg: "--max-list must be 0 or more"}
	}
	if opts.tolerance < 0 {
		return &usageError{msg: "--tolerance must not be negative"}
	}
	return nil
}

// exitCodeForError maps any error returned by ExecuteC to the process
// exit code. Typed exit errors carry their own code; usage errors (and
// Cobra's own flag, required-flag, and argument errors, which are the
// only untyped errors that can surface) exit 2; runtime errors exit 1.
func exitCodeForError(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	var re *runtimeError
	if errors.As(err, &re) {
		return 1
	}
	return 2
}

// isUsageError reports whether err should be shown with the usage
// text: our usage errors plus every Cobra-generated invocation error.
func isUsageError(err error) bool {
	var ee *exitError
	var re *runtimeError
	return !errors.As(err, &ee) && !errors.As(err, &re)
}

func main() {
	cmd := newRootCmd()
	_, err := cmd.ExecuteC()
	if err == nil {
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	if isUsageError(err) {
		fmt.Fprint(os.Stderr, cmd.UsageString())
	}
	os.Exit(exitCodeForError(err))
}
