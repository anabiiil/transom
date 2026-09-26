package cmd

import (
	"testing"

	"transom/internal/scan"
)

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 999: "999 B", 1000: "1.0 kB", 1_500_000: "1.5 MB",
		123_456_789: "123 MB", 12_800_000_000: "12.8 GB", 3_000_000_000_000: "3.0 TB",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSelectItems(t *testing.T) {
	res := &scan.Result{Categories: []scan.CategoryResult{
		{ID: "user-caches", Name: "Caches", Risk: scan.RiskSafe, Items: []scan.Item{{ID: "a", Size: 1, Risk: scan.RiskSafe}}},
		{ID: "xcode", Name: "Xcode", Risk: scan.RiskSafe, Items: []scan.Item{
			{ID: "b", Size: 2, Risk: scan.RiskSafe}, {ID: "c", Size: 4, Risk: scan.RiskReview}}},
		{ID: "large-files", Name: "Large", Risk: scan.RiskCaution, Items: []scan.Item{{ID: "d", Size: 8, Risk: scan.RiskCaution}}},
	}}
	if s := selectItems(res, false, false); len(s.ids) != 3 || s.total != 7 {
		t.Fatalf("default: %+v", s)
	}
	if s := selectItems(res, false, true); len(s.ids) != 2 || s.total != 3 {
		t.Fatalf("safe-only: %+v", s)
	}
	if s := selectItems(res, true, false); len(s.ids) != 4 {
		t.Fatalf("explicit categories include caution: %+v", s)
	}
}

func TestSplitIDs(t *testing.T) {
	got := splitIDs([]string{"a,b", " c ", ""})
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
}
