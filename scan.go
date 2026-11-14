package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// entry is one file or symlink in a tree, keyed by its path relative
// to the tree root. A symlink is an entry of its own: it is never
// followed, and it is compared by its target path (Link), so Size and
// ModTime describe regular files only.
type entry struct {
	Size    int64
	ModTime time.Time
	IsLink  bool
	Link    string
}

// Problem is one file that failed the comparison. Which list of the
// Result holds it says what is wrong; Detail is the human-readable
// evidence (sizes, times, targets, or the read error). Bytes is the
// source size used for the section's byte total (0 when there is no
// meaningful size, for example a read failure).
type Problem struct {
	Path       string    `json:"path"`
	Detail     string    `json:"detail"`
	Bytes      int64     `json:"bytes"`
	SourceSize int64     `json:"source_size,omitempty"`
	BackupSize int64     `json:"backup_size,omitempty"`
	SourceMod  time.Time `json:"source_modified,omitempty"`
	BackupMod  time.Time `json:"backup_modified,omitempty"`
}

// Extra is a file or symlink present only in the backup. Extras are
// information, never problems.
type Extra struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	IsLink bool   `json:"is_link,omitempty"`
}

// Result is the full comparison outcome: the report sections in their
// fixed order, the tree totals, and the verdict.
type Result struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
	Hashed bool   `json:"hashed"`

	SourceFiles int   `json:"source_files"`
	SourceBytes int64 `json:"source_bytes"`
	BackupFiles int   `json:"backup_files"`
	BackupBytes int64 `json:"backup_bytes"`
	SourceLinks int   `json:"source_links"`
	BackupLinks int   `json:"backup_links"`

	Missing        []Problem `json:"missing"`
	Outdated       []Problem `json:"outdated"`
	SizeDiffers    []Problem `json:"size_differs"`
	ContentDiffers []Problem `json:"content_differs"`
	LinkDiffers    []Problem `json:"link_differs"`
	Unreadable     []Problem `json:"unreadable"`

	Extras      []Extra `json:"extras"`
	ExtrasBytes int64   `json:"extras_bytes"`

	Problems int    `json:"problems"`
	Verdict  string `json:"verdict"`
}

// ProblemCount totals every problem list. Extras never count.
func (r *Result) ProblemCount() int {
	return len(r.Missing) + len(r.Outdated) + len(r.SizeDiffers) +
		len(r.ContentDiffers) + len(r.LinkDiffers) + len(r.Unreadable)
}

// VerdictText is the one-line verdict for the summary.
func (r *Result) VerdictText() string {
	if n := r.ProblemCount(); n > 0 {
		return fmt.Sprintf("Backup has %d problems", n)
	}
	return "Backup is current"
}

// finalize fills the computed fields (Problems, Verdict) so both the
// text report and the JSON output carry them.
func (r *Result) finalize() {
	r.Problems = r.ProblemCount()
	r.Verdict = r.VerdictText()
}

// treeData is one walked tree: its entries by relative path, the
// totals for regular files and links, and any read failures.
type treeData struct {
	entries    map[string]entry
	files      int
	bytes      int64
	links      int
	unreadable []Problem
}

// walkTree reads one tree without ever following a symlinked
// directory: a symlinked directory is recorded as a link entry and
// not descended into. Anything that cannot be stated, read, or
// listed is recorded as an Unreadable problem and the walk carries
// on, so one bad corner never hides the state of the rest.
func walkTree(side, root string) *treeData {
	t := &treeData{entries: map[string]entry{}}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if err != nil {
			if rel != "." {
				t.unreadable = append(t.unreadable, Problem{
					Path:   filepath.ToSlash(rel),
					Detail: fmt.Sprintf("in %s: %v", side, err),
				})
			}
			return nil
		}
		if rel == "." {
			return nil
		}
		mode := d.Type()
		switch {
		case mode.IsDir():
			return nil // descend
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				t.unreadable = append(t.unreadable, Problem{
					Path:   filepath.ToSlash(rel),
					Detail: fmt.Sprintf("in %s: %v", side, err),
				})
				return nil
			}
			t.entries[filepath.ToSlash(rel)] = entry{IsLink: true, Link: target}
			t.links++
			return nil
		case mode.IsRegular():
			info, err := d.Info()
			if err != nil {
				t.unreadable = append(t.unreadable, Problem{
					Path:   filepath.ToSlash(rel),
					Detail: fmt.Sprintf("in %s: %v", side, err),
				})
				return nil
			}
			t.entries[filepath.ToSlash(rel)] = entry{Size: info.Size(), ModTime: info.ModTime()}
			t.files++
			t.bytes += info.Size()
			return nil
		default:
			// Sockets, devices, pipes and the like are not files a
			// backup can hold; they are ignored, not problems.
			return nil
		}
	})
	return t
}

// checkBackup walks both trees and classifies every source entry
// against its backup counterpart. Classification is exclusive and in
// a fixed order (missing, link/type, size, outdated, content), so a
// file is never counted twice.
func checkBackup(opts *options) (*Result, error) {
	src := walkTree("source", opts.source)
	bak := walkTree("backup", opts.backup)

	res := &Result{
		Source:      opts.source,
		Backup:      opts.backup,
		Hashed:      opts.hash,
		SourceFiles: src.files,
		SourceBytes: src.bytes,
		BackupFiles: bak.files,
		BackupBytes: bak.bytes,
		SourceLinks: src.links,
		BackupLinks: bak.links,
	}
	res.Unreadable = append(res.Unreadable, src.unreadable...)
	res.Unreadable = append(res.Unreadable, bak.unreadable...)

	type hashJob struct {
		rel string
		src entry
		bak entry
	}
	var jobs []hashJob

	// Sorted iteration keeps every list in path order.
	for _, rel := range sortedKeys(src.entries) {
		s := src.entries[rel]
		b, present := bak.entries[rel]
		if !present {
			detail := humanBytes(s.Size)
			if s.IsLink {
				detail = "link to " + s.Link
			}
			res.Missing = append(res.Missing, Problem{Path: rel, Detail: detail, Bytes: s.Size,
				SourceSize: s.Size, SourceMod: s.ModTime})
			continue
		}
		switch {
		case s.IsLink || b.IsLink:
			if s.IsLink && b.IsLink && s.Link == b.Link {
				// Same link: fine.
			} else {
				res.LinkDiffers = append(res.LinkDiffers, Problem{Path: rel,
					Detail: fmt.Sprintf("source %s, backup %s", describeEntry(s), describeEntry(b))})
			}
		case s.Size != b.Size:
			res.SizeDiffers = append(res.SizeDiffers, Problem{Path: rel,
				Detail:     fmt.Sprintf("source %s, backup %s", humanBytes(s.Size), humanBytes(b.Size)),
				Bytes:      s.Size,
				SourceSize: s.Size, BackupSize: b.Size,
				SourceMod: s.ModTime, BackupMod: b.ModTime})
		case s.ModTime.After(b.ModTime.Add(opts.tolerance)):
			res.Outdated = append(res.Outdated, Problem{Path: rel,
				Detail:     fmt.Sprintf("source modified %s, backup modified %s", formatTime(s.ModTime), formatTime(b.ModTime)),
				Bytes:      s.Size,
				SourceSize: s.Size, BackupSize: b.Size,
				SourceMod: s.ModTime, BackupMod: b.ModTime})
		case opts.hash:
			jobs = append(jobs, hashJob{rel: rel, src: s, bak: b})
		}
	}

	if opts.hash && len(jobs) > 0 {
		paths := make([][2]string, len(jobs))
		for i, j := range jobs {
			paths[i] = [2]string{
				filepath.Join(opts.source, filepath.FromSlash(j.rel)),
				filepath.Join(opts.backup, filepath.FromSlash(j.rel)),
			}
		}
		outcomes := hashPairs(paths, opts.workers)
		for i, j := range jobs {
			switch {
			case outcomes[i].err != nil:
				res.Unreadable = append(res.Unreadable, Problem{Path: j.rel,
					Detail: fmt.Sprintf("could not hash: %v", outcomes[i].err)})
			case !outcomes[i].equal:
				res.ContentDiffers = append(res.ContentDiffers, Problem{Path: j.rel,
					Detail:     "same size, different content (SHA-256)",
					Bytes:      j.src.Size,
					SourceSize: j.src.Size, BackupSize: j.bak.Size,
					SourceMod: j.src.ModTime, BackupMod: j.bak.ModTime})
			}
		}
	}

	for _, rel := range sortedKeys(bak.entries) {
		if _, ok := src.entries[rel]; ok {
			continue
		}
		b := bak.entries[rel]
		res.Extras = append(res.Extras, Extra{Path: rel, Size: b.Size, IsLink: b.IsLink})
		res.ExtrasBytes += b.Size
	}

	sortProblems(res.Missing)
	sortProblems(res.Outdated)
	sortProblems(res.SizeDiffers)
	sortProblems(res.ContentDiffers)
	sortProblems(res.LinkDiffers)
	sortProblems(res.Unreadable)
	sortExtras(res.Extras)
	return res, nil
}

// describeEntry names an entry for link-comparison details.
func describeEntry(e entry) string {
	if e.IsLink {
		return "link to " + e.Link
	}
	return "a regular file"
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
