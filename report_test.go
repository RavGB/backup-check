package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapList(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	cases := []struct {
		max       int
		wantShown int
		wantMore  int
	}{
		{50, 4, 0},
		{4, 4, 0},
		{2, 2, 2},
		{1, 1, 3},
		{0, 0, 4},
	}
	for _, c := range cases {
		shown, more := capList(items, c.max)
		if len(shown) != c.wantShown || more != c.wantMore {
			t.Errorf("capList(max=%d) = %d shown, %d more; want %d/%d",
				c.max, len(shown), more, c.wantShown, c.wantMore)
		}
	}
	if shown, more := capList(nil, 5); len(shown) != 0 || more != 0 {
		t.Errorf("capList(nil) = %d/%d, want 0/0", len(shown), more)
	}
}

func TestRenderTextCappedAndOrdered(t *testing.T) {
	r := &Result{
		Source: "src", Backup: "bak",
		Missing: []Problem{
			{Path: "a.txt", Detail: "1 byte", Bytes: 1},
			{Path: "b.txt", Detail: "2 bytes", Bytes: 2},
			{Path: "c.txt", Detail: "3 bytes", Bytes: 3},
		},
	}
	r.finalize()
	out := renderText(r, 1)
	if !strings.Contains(out, "and 2 more") {
		t.Errorf("capped list missing 'and 2 more':\n%s", out)
	}
	if !strings.Contains(out, "Missing (3, 6 bytes)") {
		t.Errorf("missing section header with count/bytes:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: Backup has 3 problems") {
		t.Errorf("verdict line missing:\n%s", out)
	}
	// Fixed section order: Missing before Extras before Summary.
	iM, iE, iS := strings.Index(out, "Missing"), strings.Index(out, "Extras"), strings.Index(out, "Summary")
	if !(iM >= 0 && iM < iE && iE < iS) {
		t.Errorf("section order wrong: Missing=%d Extras=%d Summary=%d", iM, iE, iS)
	}
}

func TestJSONValidity(t *testing.T) {
	r := &Result{
		Source: "src", Backup: "bak", Hashed: true,
		SourceFiles: 1, SourceBytes: 4,
		Missing: []Problem{{Path: "x.txt", Detail: "4 bytes", Bytes: 4}},
		Extras:  []Extra{{Path: "old.txt", Size: 9}},
	}
	r.finalize()
	data, err := marshalResult(r)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid JSON: %s", data)
	}
	var back Result
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Problems != 1 || back.Verdict != "Backup has 1 problems" {
		t.Errorf("round trip problems/verdict = %d/%q", back.Problems, back.Verdict)
	}
	if len(back.Missing) != 1 || back.Missing[0].Path != "x.txt" {
		t.Errorf("round trip missing = %+v", back.Missing)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:    "0 bytes",
		1:    "1 byte",
		512:  "512 bytes",
		1024: "1.0 KiB",
		1536: "1.5 KiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
