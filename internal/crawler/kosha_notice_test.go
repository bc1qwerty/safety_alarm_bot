package crawler

import (
	"testing"
)

// These tests lock in the rules buildKoshaPosts must follow so the
// title/URL pairing and the dedup-critical display-number ("No") scheme
// cannot silently regress.
//
// Any change to the function must keep all scenarios green:
//   - no stickies, only regulars
//   - 1 sticky followed by regulars (the production-observed shape)
//   - multiple stickies followed by regulars
//   - a regular row whose pstNo has no title in bbsPstGrid must be dropped,
//     not emitted with an empty title
//   - empty input emits nothing

func TestBuildKoshaPosts_NoStickies(t *testing.T) {
	grid := []koshaPstNoItem{
		{Rnum: 1, PstNo: "P100", TotalCount: 100},
		{Rnum: 2, PstNo: "P99", TotalCount: 100},
		{Rnum: 3, PstNo: "P98", TotalCount: 100},
	}
	bbs := []koshaBbsPstItem{
		{PstNo: "P100", PstNm: "post-100"},
		{PstNo: "P99", PstNm: "post-99"},
		{PstNo: "P98", PstNm: "post-98"},
	}

	got := buildKoshaPosts(grid, bbs, 100, "src", "U?p=")

	want := []struct{ id, title, url string }{
		{"100", "post-100", "U?p=P100"},
		{"99", "post-99", "U?p=P99"},
		{"98", "post-98", "U?p=P98"},
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].PostID != w.id || got[i].Title != w.title || got[i].URL != w.url {
			t.Errorf("[%d]: got %+v want id=%s title=%s url=%s", i, got[i], w.id, w.title, w.url)
		}
	}
}

func TestBuildKoshaPosts_OneStickyFilteredOut(t *testing.T) {
	// Production shape: one pinned sticky (totalCount=1) ahead of regular
	// posts (totalCount=totalNormalCnt). Both groups restart rnum at 1.
	grid := []koshaPstNoItem{
		{Rnum: 1, PstNo: "STICKY", TotalCount: 1},
		{Rnum: 1, PstNo: "P2742", TotalCount: 2742},
		{Rnum: 2, PstNo: "P2741", TotalCount: 2742},
		{Rnum: 3, PstNo: "P2740", TotalCount: 2742},
	}
	bbs := []koshaBbsPstItem{
		{PstNo: "STICKY", PstNm: "sticky: should be filtered out"},
		{PstNo: "P2742", PstNm: "regular-2742"},
		{PstNo: "P2741", PstNm: "regular-2741"},
		{PstNo: "P2740", PstNm: "regular-2740"},
	}

	got := buildKoshaPosts(grid, bbs, 2742, "src", "U?p=")

	want := map[string]string{
		"2742": "U?p=P2742",
		"2741": "U?p=P2741",
		"2740": "U?p=P2740",
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for _, p := range got {
		wantURL, ok := want[p.PostID]
		if !ok {
			t.Errorf("unexpected PostID %q (sticky leaked?)", p.PostID)
			continue
		}
		if p.URL != wantURL {
			t.Errorf("PostID %s: got URL %q want %q", p.PostID, p.URL, wantURL)
		}
	}
}

func TestBuildKoshaPosts_MultipleStickies(t *testing.T) {
	// Defensive: 3-sticky configuration. Each sticky has its own totalCount=1
	// entry; regulars share totalCount=500. Only the regulars are emitted.
	grid := []koshaPstNoItem{
		{Rnum: 1, PstNo: "S1", TotalCount: 1},
		{Rnum: 1, PstNo: "S2", TotalCount: 1},
		{Rnum: 1, PstNo: "S3", TotalCount: 1},
		{Rnum: 1, PstNo: "R500", TotalCount: 500},
		{Rnum: 2, PstNo: "R499", TotalCount: 500},
		{Rnum: 3, PstNo: "R498", TotalCount: 500},
	}
	bbs := []koshaBbsPstItem{
		{PstNo: "S1", PstNm: "s1"}, {PstNo: "S2", PstNm: "s2"}, {PstNo: "S3", PstNm: "s3"},
		{PstNo: "R500", PstNm: "r500"}, {PstNo: "R499", PstNm: "r499"}, {PstNo: "R498", PstNm: "r498"},
	}

	got := buildKoshaPosts(grid, bbs, 500, "src", "U?p=")
	if len(got) != 3 {
		t.Fatalf("len: got %d want 3", len(got))
	}
	expect := []struct{ id, url string }{
		{"500", "U?p=R500"}, {"499", "U?p=R499"}, {"498", "U?p=R498"},
	}
	for i, e := range expect {
		if got[i].PostID != e.id || got[i].URL != e.url {
			t.Errorf("[%d]: got id=%s url=%q want id=%s url=%q", i, got[i].PostID, got[i].URL, e.id, e.url)
		}
	}
}

func TestBuildKoshaPosts_MissingTitleSkipsRow(t *testing.T) {
	// A regular pstNo absent from bbsPstGrid must be dropped, not emitted with
	// an empty title.
	grid := []koshaPstNoItem{
		{Rnum: 1, PstNo: "P100", TotalCount: 100},
		{Rnum: 2, PstNo: "P99", TotalCount: 100},
		{Rnum: 3, PstNo: "P98", TotalCount: 100},
	}
	bbs := []koshaBbsPstItem{
		{PstNo: "P100", PstNm: "good-100"},
		{PstNo: "P98", PstNm: "good-98"}, // P99 intentionally missing
	}

	got := buildKoshaPosts(grid, bbs, 100, "src", "U?p=")

	if len(got) != 2 {
		t.Fatalf("len: got %d want 2 (row without a title must be dropped)", len(got))
	}
	if got[0].PostID != "100" || got[0].URL != "U?p=P100" {
		t.Errorf("[0]: %+v", got[0])
	}
	if got[1].PostID != "98" || got[1].URL != "U?p=P98" {
		t.Errorf("[1]: %+v", got[1])
	}
}

func TestBuildKoshaPosts_EmptyEmitsNothing(t *testing.T) {
	if got := buildKoshaPosts(nil, nil, 0, "src", "U?p="); len(got) != 0 {
		t.Fatalf("expected 0 posts for empty input, got %d", len(got))
	}
}
