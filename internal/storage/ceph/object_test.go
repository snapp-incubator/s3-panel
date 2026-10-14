package ceph

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

// fakeS3 answers ListObjectsV2 over an in-memory, sorted key set, and counts
// the calls it served — which is the thing these tests are about: a page of a
// listing must cost a bounded number of gateway calls, however big the bucket.
type fakeS3 struct {
	keys  []string
	calls atomic.Int64
}

type fakeListResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken,omitempty"`
	Contents              []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
}

func newFakeS3(t *testing.T, keys []string) (*fakeS3, config.ObjectStorageConfig) {
	t.Helper()
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	f := &fakeS3{keys: sorted}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, config.ObjectStorageConfig{URL: srv.URL}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	q := r.URL.Query()
	prefix, delimiter := q.Get("prefix"), q.Get("delimiter")
	maxKeys, _ := strconv.Atoi(q.Get("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	// The continuation token is opaque to the client; here it is simply the
	// last entry (key or common prefix) the previous page returned.
	after := q.Get("start-after")
	if tok := q.Get("continuation-token"); tok != "" {
		after = tok
	}

	var out fakeListResult
	n := 0
	last := ""
	for _, key := range f.keys {
		if !strings.HasPrefix(key, prefix) || key <= after {
			continue
		}
		entry, isPrefix := key, false
		if delimiter != "" {
			if i := strings.Index(key[len(prefix):], delimiter); i >= 0 {
				entry, isPrefix = key[:len(prefix)+i+len(delimiter)], true
			}
		}
		// Everything under a common prefix the previous page already returned
		// rolls up into it, so it is skipped rather than returned again.
		if isPrefix && (entry == last || entry == after) {
			continue
		}
		if n == maxKeys {
			out.IsTruncated = true
			out.NextContinuationToken = last
			break
		}
		if isPrefix {
			out.CommonPrefixes = append(out.CommonPrefixes, struct {
				Prefix string `xml:"Prefix"`
			}{entry})
		} else {
			out.Contents = append(out.Contents, struct {
				Key          string    `xml:"Key"`
				Size         int64     `xml:"Size"`
				LastModified time.Time `xml:"LastModified"`
			}{Key: key, Size: 1, LastModified: time.Unix(0, 0).UTC()})
		}
		last = entry
		n++
	}

	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(out)
}

func manyKeys(n int, format string) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf(format, i)
	}
	return keys
}

func names(items []storage.ObjectListBody) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = *it.Name
	}
	return out
}

func list(t *testing.T, cfg config.ObjectStorageConfig, meta storage.ObjectListRequestMeta) storage.ObjectListResponse {
	t.Helper()
	meta.Bucket = "b"
	meta.AccessKey, meta.SecretKey = "ak", "sk"
	out, errList := CephObjectStorage{}.ObjectList(cfg, meta)
	if errList.Message != nil {
		t.Fatalf("ObjectList: %v", errList.Message)
	}
	return out
}

// TestListingAPageOfAHugeBucketIsOneCall is the hang: listing used to walk every
// key at the level to count them before returning a single page, so a bucket
// with millions of objects never answered at all.
func TestListingAPageOfAHugeBucketIsOneCall(t *testing.T) {
	f, cfg := newFakeS3(t, manyKeys(50_000, "obj-%06d"))

	out := list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20})

	if got := f.calls.Load(); got != 1 {
		t.Fatalf("first page took %d gateway calls, want 1", got)
	}
	if len(out.Items) != 20 || *out.Items[0].Name != "obj-000000" {
		t.Fatalf("first page = %v", names(out.Items))
	}
	if !out.IsTruncated || out.NextContinuationToken == "" {
		t.Fatalf("a bucket with more objects must say so and hand back a token: %+v", out)
	}
}

// TestContinuationTokenResumesWhereThePageEnded walks a level with folders and
// files together and checks every entry is seen exactly once across pages.
func TestContinuationTokenResumesWhereThePageEnded(t *testing.T) {
	keys := append(manyKeys(30, "dir-%02d/file"), manyKeys(25, "file-%02d")...)
	f, cfg := newFakeS3(t, keys)

	var seen []string
	token := ""
	for page := 1; ; page++ {
		out := list(t, cfg, storage.ObjectListRequestMeta{Page: int32(page), MaxKeys: 20, ContinuationToken: token})
		seen = append(seen, names(out.Items)...)
		if !out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}

	if len(seen) != 55 {
		t.Fatalf("saw %d entries across pages, want 55 (30 folders + 25 files): %v", len(seen), seen)
	}
	if seen[0] != "dir-00/" || seen[54] != "file-24" {
		t.Fatalf("unexpected order: first %q last %q", seen[0], seen[54])
	}
	if got := f.calls.Load(); got != 3 {
		t.Fatalf("three pages took %d gateway calls, want 3", got)
	}
}

// TestPageNumberWithoutTokenStillWorks keeps a caller that pages by number
// working: it costs one gateway call per page skipped, never a whole-bucket walk.
func TestPageNumberWithoutTokenStillWorks(t *testing.T) {
	f, cfg := newFakeS3(t, manyKeys(50_000, "obj-%06d"))

	out := list(t, cfg, storage.ObjectListRequestMeta{Page: 3, MaxKeys: 20})

	if len(out.Items) != 20 || *out.Items[0].Name != "obj-000040" {
		t.Fatalf("page 3 = %v", names(out.Items))
	}
	if got := f.calls.Load(); got != 3 {
		t.Fatalf("page 3 took %d gateway calls, want 3", got)
	}
}

// TestFolderListingIsRelativeToThePrefix keeps what the object table relies on:
// names inside a folder come back without the folder's own prefix, and the
// folder's placeholder object is not listed as a file of itself.
func TestFolderListingIsRelativeToThePrefix(t *testing.T) {
	_, cfg := newFakeS3(t, []string{"docs/", "docs/a.txt", "docs/sub/b.txt", "other"})

	out := list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20, Prefix: "docs/"})

	got := strings.Join(names(out.Items), ",")
	if got != "sub/,a.txt" {
		t.Fatalf("docs/ listing = %q, want %q", got, "sub/,a.txt")
	}
	if !out.Items[0].IsFolder || out.Items[1].IsFolder {
		t.Fatalf("folder flags wrong: %+v", out.Items)
	}
}

// TestSearchIsBoundedPerRequest: a substring search cannot use an index, so it
// has to look at keys — but a single request may only look at so many, and must
// hand back where it stopped so the next request can carry on.
func TestSearchIsBoundedPerRequest(t *testing.T) {
	keys := manyKeys(50_000, "obj-%06d")
	keys = append(keys, "zz-needle")
	f, cfg := newFakeS3(t, keys)

	out := list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20, SearchString: "needle"})

	if got := f.calls.Load(); got > int64(searchScanBudget/searchScanPage) {
		t.Fatalf("one search request took %d gateway calls; the budget allows %d", got, searchScanBudget/searchScanPage)
	}
	if len(out.Items) != 0 || !out.IsTruncated || out.NextContinuationToken == "" {
		t.Fatalf("the match is past the budget, so this page must be empty but resumable: %+v", out)
	}

	// Following the token eventually reaches the match, and never repeats a key.
	token := out.NextContinuationToken
	var found []string
	for range 20 {
		out = list(t, cfg, storage.ObjectListRequestMeta{Page: 2, MaxKeys: 20, SearchString: "needle", ContinuationToken: token})
		found = append(found, names(out.Items)...)
		if !out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	if strings.Join(found, ",") != "zz-needle" {
		t.Fatalf("search found %v, want [zz-needle]", found)
	}
}

// TestSearchStopsAtAFullPageAndResumesExactly: when a page fills up part-way
// through a gateway listing, the rest of that listing must not be skipped.
func TestSearchStopsAtAFullPageAndResumesExactly(t *testing.T) {
	_, cfg := newFakeS3(t, manyKeys(45, "match-%02d"))

	first := list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20, SearchString: "match"})
	second := list(t, cfg, storage.ObjectListRequestMeta{Page: 2, MaxKeys: 20, SearchString: "match", ContinuationToken: first.NextContinuationToken})
	third := list(t, cfg, storage.ObjectListRequestMeta{Page: 3, MaxKeys: 20, SearchString: "match", ContinuationToken: second.NextContinuationToken})

	all := append(append(names(first.Items), names(second.Items)...), names(third.Items)...)
	if len(all) != 45 || all[20] != "match-20" || all[44] != "match-44" {
		t.Fatalf("pages = %v", all)
	}
	if third.IsTruncated {
		t.Fatalf("the last page must not claim more")
	}
}

// TestSearchIsScopedToTheCurrentFolder: searching inside a folder looks only
// under it, and matches on the part of the key below the folder.
func TestSearchIsScopedToTheCurrentFolder(t *testing.T) {
	_, cfg := newFakeS3(t, []string{"logs/a-report.txt", "logs/b.txt", "reports/2024/q1.txt", "reports/report.txt"})

	out := list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20, Prefix: "logs/", SearchString: "report"})
	if got := strings.Join(names(out.Items), ","); got != "logs/a-report.txt" {
		t.Fatalf("search in logs/ = %q, want only logs/a-report.txt", got)
	}

	// Inside reports/, the folder's own name must not make every key match.
	out = list(t, cfg, storage.ObjectListRequestMeta{Page: 1, MaxKeys: 20, Prefix: "reports/", SearchString: "report"})
	if got := strings.Join(names(out.Items), ","); got != "reports/report.txt" {
		t.Fatalf("search in reports/ = %q, want only reports/report.txt", got)
	}
}
