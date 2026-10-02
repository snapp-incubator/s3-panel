package storage

// BucketActionRequestMeta used for APIs that need the "bucket" name to take actions, like "Create", "Delete"
type BucketActionRequestMeta struct {
	AccessKey string `header:"access_key" validate:"required"`
	SecretKey string `header:"secret_key" validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string `header:"session_token"`
	Bucket       string `query:"bucket"      validate:"required"`
}

type BucketListAndQuotaRequestMeta struct {
	AccessKey string `header:"access_key"   validate:"required"`
	SecretKey string `header:"secret_key"   validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string `header:"session_token"`
	MaxKeys      int32  `query:"max_keys" validate:"required"`
	Page         int32  `query:"page" validate:"required"`
	SearchString string `query:"search_string"`
	UID          string
}

type BucketQuotaResponse struct {
	Items        []SingleBucketQuotaResponse `json:"items"`
	TotalPages   int                         `json:"total_pages"`
	TotalBuckets int                         `json:"total_buckets"`
}

type SingleBucketQuotaResponse struct {
	BucketName      string  `json:"bucket"`
	QuotaEnabled    *bool   `json:"quota_enabled"`
	UsedBytes       float64 `json:"used_bytes"`
	UsedBytesUnit   string  `json:"used_bytes_unit"`
	UsedBytesRaw    *uint64 `json:"used_bytes_raw"`
	HardBytes       float64 `json:"hard_bytes"`
	HardBytesUnit   string  `json:"hard_bytes_unit"`
	HardBytesRaw    *int64  `json:"hard_bytes_raw"`
	UsedObjects     int     `json:"used_objects"`
	HardObjects     *int64  `json:"hard_objects"`
	ModifyTimeStamp string  `json:"modify_time_stamp"`
	Tenant          string  `json:"tenant"`
	Access          string  `json:"access"`
}

type BucketListResponse struct {
	Items        []string `json:"items"`
	TotalPages   int      `json:"total_pages"`
	TotalBuckets int      `json:"total_buckets"`
}

type BucketCreateResponse struct {
	Created bool `json:"created"`
}

type BucketDeleteResponse struct {
	Deleted    bool `json:"deleted"`
	HasObjects bool `json:"has_objects"`
}
