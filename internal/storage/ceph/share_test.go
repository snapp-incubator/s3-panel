package ceph

import (
	"testing"
	"time"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

// TestShareReportsTheEffectiveExpiry: the answer names when the link actually
// stops working — after the cap — and that instant is the one signed into it.
// Presigning is local, so no gateway is contacted.
func TestShareReportsTheEffectiveExpiry(t *testing.T) {
	cfg := config.ObjectStorageConfig{URL: "http://127.0.0.1:1"}
	meta := storage.ObjectRequestMeta{
		AccessKey: "AK", SecretKey: "SK", Bucket: "b", Object: "o",
		Expiration: "1w", MaxExpiration: 20 * time.Minute,
	}

	before := time.Now().Add(-time.Second)
	res, errShare := CephObjectStorage{}.ObjectShare(cfg, meta)
	if errShare.Message != nil {
		t.Fatalf("ObjectShare: %v", errShare.Message)
	}
	if res.ExpiresInSeconds != int64((20 * time.Minute).Seconds()) {
		t.Errorf("expires_in_seconds = %d, want the capped 1200, not the week asked for", res.ExpiresInSeconds)
	}
	at, err := time.Parse(time.RFC3339, res.ExpiresAt)
	if err != nil {
		t.Fatalf("expires_at %q: %v", res.ExpiresAt, err)
	}
	if !at.Equal(presignedExpiry(res.URL, time.Time{})) {
		t.Errorf("expires_at %v is not the expiry signed into the URL", at)
	}
	if at.Before(before.Add(20*time.Minute)) || at.After(time.Now().Add(20*time.Minute)) {
		t.Errorf("expires_at = %v, want about 20 minutes from now", at)
	}

	// Uncapped (s3 mode, no share.max_duration): the duration asked for stands.
	meta.MaxExpiration = 0
	meta.Expiration = "6h"
	res, errShare = CephObjectStorage{}.ObjectShare(cfg, meta)
	if errShare.Message != nil {
		t.Fatalf("ObjectShare: %v", errShare.Message)
	}
	if res.ExpiresInSeconds != int64((6 * time.Hour).Seconds()) {
		t.Errorf("expires_in_seconds = %d, want 6h", res.ExpiresInSeconds)
	}
}

func TestPresignedExpiry(t *testing.T) {
	fallback := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	got := presignedExpiry("https://s3.example/b/o?X-Amz-Date=20261121T101500Z&X-Amz-Expires=3600&X-Amz-Signature=x", fallback)
	if want := time.Date(2026, 11, 21, 11, 15, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("presignedExpiry = %v, want %v", got, want)
	}
	for _, raw := range []string{
		"https://s3.example/b/o",
		"https://s3.example/b/o?X-Amz-Date=bad&X-Amz-Expires=60",
		"https://s3.example/b/o?X-Amz-Date=20261121T101500Z&X-Amz-Expires=0",
		"://not a url",
	} {
		if got := presignedExpiry(raw, fallback); !got.Equal(fallback) {
			t.Errorf("presignedExpiry(%q) = %v, want the fallback", raw, got)
		}
	}
}
