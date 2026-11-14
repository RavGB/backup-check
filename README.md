# backup-check

**Verify that a backup folder really matches the original: nothing missing, nothing out of date, nothing the wrong size.**

Everyone has a backup folder. Almost nobody knows whether it is actually good. Files get missed, a copy is taken before the final edit, or a file is the right size with the wrong contents. backup-check compares an original folder with its backup, file by file, and gives a plain verdict: "Backup is current", or a list of exactly what is wrong.

```text
./backup-check --source ~/Documents --backup /mnt/backup/Documents
./backup-check --source ~/Documents --backup /mnt/backup/Documents --hash
```

## Build

Go, with the Cobra command-line library. No other dependencies.

```text
go build -o backup-check .
```

## Usage

```text
backup-check --source DIR --backup DIR [flags]
```

Both folders are required. A file's path inside its tree is its identity: `docs/report.txt` in the source is compared with `docs/report.txt` in the backup. Every source file gets exactly one classification, checked in this order:

1. **Missing**: no backup copy at all.
2. **Link/type differs**: one side is a symlink and the other is not, or two symlinks point at different targets. Symlinks are never followed, not even symlinked directories; a symlink is an entry of its own and is compared by its target path.
3. **Size differs**: the two copies have different sizes.
4. **Outdated**: same size, but the source copy was modified after the backup copy by more than `--tolerance` (default 2s, there to absorb timestamp rounding between filesystems). A backup copy newer than the source is never "outdated".
5. **Content differs**: only with `--hash`. Same size and time looks fine, but the SHA-256 of the contents differs. This is the slow, certain check; use it for backups that matter.

Anything that cannot be read (a file the walk cannot state, a folder it cannot list, a file that fails while hashing) is counted as a problem under **Unreadable**, because a backup that cannot be read cannot be trusted. The scan never stops at the first bad file.

Files present only in the backup are **Extras**: they are listed with a total count and bytes, as information. Extras are never counted as problems, because people keep older files in backups on purpose.

Empty directories are not compared. Files and links are what a backup holds; a folder with nothing in it carries nothing to lose.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--source` | (required) | The original folder. |
| `--backup` | (required) | The backup folder to verify. |
| `--hash` | off | Also compare contents with SHA-256. |
| `--workers` | 4 | Files hashed in parallel when `--hash` is set. |
| `--tolerance` | 2s | How much newer the source may be before a file counts as outdated. A Go duration: `500ms`, `2s`, `1m`. |
| `--max-list` | 50 | Most entries shown per report section; the rest become "and M more". Counts and byte totals always cover everything. 0 shows counts only. In `--json` output nothing is ever capped. |
| `--json` | off | Print the full result as JSON instead of text. |
| `--output` | | Also write the text report to this file. |

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | The backup is current: no problems. |
| 1 | At least one problem (missing, outdated, size, content, link, or unreadable). |
| 2 | The command was used incorrectly: a missing flag, a folder that does not exist, a bad duration, a report path that cannot be written. Usage is shown only for these. |

The exit code makes backup-check easy to run on a schedule: a nightly run that exits 1 is a backup that needs attention.

## Sample output

A real run on this machine, against a small demo pair (an original with four files; its backup is missing one, holds a nine-day-old copy of another, has one file with the same size but different bytes, and keeps one extra old file):

```text
$ ./backup-check --source /tmp/bcheck-demo/original --backup /tmp/bcheck-demo/backup --hash
Backup check
  Source: /tmp/bcheck-demo/original
  Backup: /tmp/bcheck-demo/backup
  Content compared with SHA-256.

Missing (1, 29 bytes):
  notes.txt (29 bytes)

Outdated (1, 33 bytes):
  docs/report.txt (source modified 2026-11-10 10:00:00, backup modified 2026-11-01 10:00:00)

Size differs: none

Content differs (1, 4 bytes):
  data.bin (same size, different content (SHA-256))

Link/type differs: none

Unreadable: none

Extras (1, 25 bytes) - only in the backup, kept for information:
  old-list.txt (25 bytes)

Summary
  Source: 4 files, 87 bytes
  Backup: 4 files, 83 bytes
  Extras in backup: 1 (25 bytes), not problems
  Problems: 3
Verdict: Backup has 3 problems
```

Exit code was 1. Without `--hash` the same run finds 2 problems (data.bin looks fine by size and time); a clean pair reports "Verdict: Backup is current" and exits 0.

## License

MIT. See [LICENSE](LICENSE).
