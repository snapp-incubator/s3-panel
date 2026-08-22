package api

import "testing"

// TestTotalPagesMatchesLegacyArithmetic pins the paging the iam-mode listing
// computes itself against the arithmetic the s3-mode path does inline, so the
// two cannot drift apart.
func TestTotalPagesMatchesLegacyArithmetic(t *testing.T) {
	cases := []struct{ matched, perPage, want int }{
		{0, 100, 0},
		{1, 100, 1},
		{100, 100, 1},
		{101, 100, 2},
		{250, 100, 3},
		{5, 0, 0}, // guard: a zero page size must not divide by zero
	}
	for _, tc := range cases {
		if got := totalPages(tc.matched, tc.perPage); got != tc.want {
			t.Errorf("totalPages(%d, %d) = %d, want %d", tc.matched, tc.perPage, got, tc.want)
		}
	}
}
