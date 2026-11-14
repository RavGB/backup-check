package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// humanBytes formats a byte count for people: bytes below a KiB stay
// in bytes, larger values get one decimal (1024-based).
func humanBytes(n int64) string {
	if n == 1 {
		return "1 byte"
	}
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v := float64(n)
	for _, u := range units {
		v /= 1024
		if v < 1024 {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f EiB", v)
}

// capList applies the --max-list display cap: it returns the entries
// to show and how many were left out. A max of 0 shows none. The data
// itself is never capped, only the printed lists.
func capList(items []string, max int) (shown []string, more int) {
	if max < 0 {
		max = 0
	}
	if len(items) <= max {
		return items, 0
	}
	return items[:max], len(items) - max
}

// marshalResult renders the full result as indented JSON. JSON is
// never capped by --max-list: it is the machine-readable record.
func marshalResult(r *Result) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// renderText builds the human report. Sections appear in the fixed
// order Missing, Outdated, Size differs, Content differs, Link/type
// differs, Unreadable, Extras, then the summary and verdict. A
// problem section with nothing in it collapses to one "none" line;
// Extras are information and appear with their count and bytes.
func renderText(r *Result, maxList int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Backup check\n  Source: %s\n  Backup: %s\n", r.Source, r.Backup)
	if r.Hashed {
		b.WriteString("  Content compared with SHA-256.\n")
	}
	b.WriteString("\n")

	writeProblemSection(&b, "Missing", r.Missing, maxList)
	writeProblemSection(&b, "Outdated", r.Outdated, maxList)
	writeProblemSection(&b, "Size differs", r.SizeDiffers, maxList)
	writeProblemSection(&b, "Content differs", r.ContentDiffers, maxList)
	writeProblemSection(&b, "Link/type differs", r.LinkDiffers, maxList)
	writeProblemSection(&b, "Unreadable", r.Unreadable, maxList)

	if len(r.Extras) == 0 {
		b.WriteString("Extras: none\n\n")
	} else {
		fmt.Fprintf(&b, "Extras (%d, %s) - only in the backup, kept for information:\n",
			len(r.Extras), humanBytes(r.ExtrasBytes))
		lines := make([]string, len(r.Extras))
		for i, e := range r.Extras {
			detail := humanBytes(e.Size)
			if e.IsLink {
				detail = "link"
			}
			lines[i] = fmt.Sprintf("  %s (%s)", e.Path, detail)
		}
		shown, more := capList(lines, maxList)
		for _, l := range shown {
			b.WriteString(l + "\n")
		}
		if more > 0 {
			fmt.Fprintf(&b, "  and %d more\n", more)
		}
		b.WriteString("\n")
	}

	b.WriteString("Summary\n")
	fmt.Fprintf(&b, "  Source: %d files, %s", r.SourceFiles, humanBytes(r.SourceBytes))
	if r.SourceLinks > 0 {
		fmt.Fprintf(&b, ", %d links", r.SourceLinks)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  Backup: %d files, %s", r.BackupFiles, humanBytes(r.BackupBytes))
	if r.BackupLinks > 0 {
		fmt.Fprintf(&b, ", %d links", r.BackupLinks)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  Extras in backup: %d (%s), not problems\n", len(r.Extras), humanBytes(r.ExtrasBytes))
	fmt.Fprintf(&b, "  Problems: %d\n", r.ProblemCount())
	fmt.Fprintf(&b, "Verdict: %s\n", r.VerdictText())
	return b.String()
}

// writeProblemSection renders one problem list with its count and
// byte total, capped by --max-list with an "and M more" line.
func writeProblemSection(b *strings.Builder, name string, items []Problem, maxList int) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n\n", name)
		return
	}
	var bytes int64
	for _, p := range items {
		bytes += p.Bytes
	}
	fmt.Fprintf(b, "%s (%d, %s):\n", name, len(items), humanBytes(bytes))
	lines := make([]string, len(items))
	for i, p := range items {
		lines[i] = fmt.Sprintf("  %s (%s)", p.Path, p.Detail)
	}
	shown, more := capList(lines, maxList)
	for _, l := range shown {
		b.WriteString(l + "\n")
	}
	if more > 0 {
		fmt.Fprintf(b, "  and %d more\n", more)
	}
	b.WriteString("\n")
}

// sortedKeys returns the map keys in path order so classification
// walks the tree deterministically.
func sortedKeys(m map[string]entry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortProblems(items []Problem) {
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
}

func sortExtras(items []Extra) {
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
}
