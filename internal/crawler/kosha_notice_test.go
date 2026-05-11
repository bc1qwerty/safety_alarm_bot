package crawler

import (
	"testing"
)

// These tests lock in the rules that pairKoshaPosts must follow so the
// title/URL off-by-one bug (caused by KOSHA mixing sticky and regular posts
// in baseList) cannot silently regress.
//
// Any change to the function must keep all four scenarios green:
//   - no stickies, only regulars
//   - 1 sticky followed by regulars (the production-observed shape)
//   - multiple stickies followed by regulars
//   - rows whose display number breaks the totalCount-rnum+1 invariant must
//     be dropped, not emitted with a wrong URL

func TestPairKoshaPosts_NoStickies(t *testing.T) {
	rows := []koshaRowItem{
		{Num: "100", Title: "post-100"},
		{Num: "99", Title: "post-99"},
		{Num: "98", Title: "post-98"},
	}
	base := []baseListItem{
		{Rnum: 1, PstNo: "P100", TotalCount: 100},
		{Rnum: 2, PstNo: "P99", TotalCount: 100},
		{Rnum: 3, PstNo: "P98", TotalCount: 100},
	}

	got := pairKoshaPosts(rows, base, "src", "U?p=")

	want := []struct {
		id, title, url string
	}{
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

func TestPairKoshaPosts_OneStickyMatchesProductionBug(t *testing.T) {
	// Real-world shape observed on 2026-05-11: one sticky pinned, regular
	// posts start at #2742. Previous code paired No2742 with No2741's pstNo
	// because the sticky's rnum=1 collided with the first regular's rnum=1.
	rows := []koshaRowItem{
		{Num: "STICKY", Title: "STICKY: should be filtered out"},
		{Num: "2742", Title: "regular-2742"},
		{Num: "2741", Title: "regular-2741"},
		{Num: "2740", Title: "regular-2740"},
	}
	base := []baseListItem{
		{Rnum: 1, PstNo: "STICKY_PST", TotalCount: 1},
		{Rnum: 1, PstNo: "PST_2742", TotalCount: 2742},
		{Rnum: 2, PstNo: "PST_2741", TotalCount: 2742},
		{Rnum: 3, PstNo: "PST_2740", TotalCount: 2742},
	}

	got := pairKoshaPosts(rows, base, "src", "U?p=")

	want := map[string]string{
		"2742": "U?p=PST_2742",
		"2741": "U?p=PST_2741",
		"2740": "U?p=PST_2740",
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for _, p := range got {
		wantURL, ok := want[p.PostID]
		if !ok {
			t.Errorf("unexpected PostID %q", p.PostID)
			continue
		}
		if p.URL != wantURL {
			t.Errorf("PostID %s: got URL %q want %q", p.PostID, p.URL, wantURL)
		}
	}
}

func TestPairKoshaPosts_MultipleStickies(t *testing.T) {
	// Defensive: hypothetical 3-sticky configuration. Each sticky has its own
	// totalCount=1 entry; regulars share totalCount=500. The largest-group
	// rule must still pick the regulars and produce correctly-ordered URLs.
	rows := []koshaRowItem{
		{Num: "공지", Title: "s1"},
		{Num: "공지", Title: "s2"},
		{Num: "공지", Title: "s3"},
		{Num: "500", Title: "r500"},
		{Num: "499", Title: "r499"},
		{Num: "498", Title: "r498"},
	}
	base := []baseListItem{
		{Rnum: 1, PstNo: "S1", TotalCount: 1},
		{Rnum: 1, PstNo: "S2", TotalCount: 1},
		{Rnum: 1, PstNo: "S3", TotalCount: 1},
		{Rnum: 1, PstNo: "R500", TotalCount: 500},
		{Rnum: 2, PstNo: "R499", TotalCount: 500},
		{Rnum: 3, PstNo: "R498", TotalCount: 500},
	}

	got := pairKoshaPosts(rows, base, "src", "U?p=")
	if len(got) != 3 {
		t.Fatalf("len: got %d want 3", len(got))
	}
	expect := []string{"U?p=R500", "U?p=R499", "U?p=R498"}
	for i, e := range expect {
		if got[i].URL != e {
			t.Errorf("[%d]: got URL %q want %q", i, got[i].URL, e)
		}
	}
}

func TestPairKoshaPosts_InvariantBreakSkipsBadRow(t *testing.T) {
	// Simulate KOSHA returning a row whose display number doesn't match
	// (mainTotal - regularIdx + 1) -- e.g. schema change or duplicate row.
	// Bad rows must be dropped, not paired with the wrong pstNo.
	rows := []koshaRowItem{
		{Num: "100", Title: "good-100"},
		{Num: "97", Title: "bad: should be 99"},
		{Num: "98", Title: "good-98"},
	}
	base := []baseListItem{
		{Rnum: 1, PstNo: "P100", TotalCount: 100},
		{Rnum: 2, PstNo: "P99", TotalCount: 100},
		{Rnum: 3, PstNo: "P98", TotalCount: 100},
	}

	got := pairKoshaPosts(rows, base, "src", "U?p=")

	if len(got) != 2 {
		t.Fatalf("len: got %d want 2 (one row must be dropped)", len(got))
	}
	if got[0].PostID != "100" || got[0].URL != "U?p=P100" {
		t.Errorf("[0]: %+v", got[0])
	}
	if got[1].PostID != "98" || got[1].URL != "U?p=P98" {
		t.Errorf("[1]: %+v", got[1])
	}
}

func TestPairKoshaPosts_EmptyBaseListEmitsNothing(t *testing.T) {
	rows := []koshaRowItem{
		{Num: "100", Title: "post"},
	}
	got := pairKoshaPosts(rows, nil, "src", "U?p=")
	if len(got) != 0 {
		t.Fatalf("expected 0 posts when baseList is empty, got %d", len(got))
	}
}
