package storage

import "time"

type ObjectListRequestMeta struct {
	AccessKey string `header:"access_key"   validate:"required"`
	SecretKey string `header:"secret_key"   validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string `header:"session_token"`
	Bucket       string `query:"bucket"        validate:"required"`
	Page         int32  `query:"page"          validate:"required"`
	MaxKeys      int32  `query:"max_keys"      validate:"required"`
	SearchString string `query:"search_string"`
	Prefix       string `query:"prefix"`
}

type ObjectDeleteRequestMeta struct {
	AccessKey string `header:"access_key" validate:"required"`
	SecretKey string `header:"secret_key" validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string   `header:"session_token"`
	Bucket       string   `query:"bucket"      validate:"required"`
	Objects      []string `query:"objects"      validate:"required"`
}

type ObjectRequestMeta struct {
	AccessKey string `header:"access_key" validate:"required"`
	SecretKey string `header:"secret_key" validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string `header:"session_token"`
	Bucket       string `query:"bucket"      validate:"required"`
	Object       string `query:"object"      validate:"required"`
	Expiration   string `query:"expiration"`
	// MaxExpiration caps a presigned URL's lifetime. Set by the server, never by
	// the caller: in iam mode the URL is signed with a short-lived credential and
	// stops working the moment that credential lapses, however long the caller
	// asked for. Zero means "no cap", which is the s3 mode case, where the URL is
	// signed with the user's own long-lived keys.
	MaxExpiration time.Duration
}

type ObjectUploadRequestMeta struct {
	AccessKey string `header:"access_key" validate:"required"`
	SecretKey string `header:"secret_key" validate:"required"`
	// SessionToken accompanies a short-lived credential. Empty for the
	// long-lived keys a user types in s3 mode, so it is never required.
	SessionToken string `header:"session_token"`
	Bucket       string `form:"bucket"       validate:"required"`
	Prefix       string `form:"prefix"`
}

type ObjectListBody struct {
	Name                  *string `json:"name"`
	SizeValue             float64 `json:"size_value"`
	SizeUnit              string  `json:"size_unit"`
	LastModifiedTimestamp string  `json:"last_modified_timestamp"`
	IsFolder              bool    `json:"is_folder"`
}

type ObjectListResponse struct {
	Items             []ObjectListBody `json:"items"`
	TotalMatchedItems int              `json:"total_matched_items"`
	TotalPages        int              `json:"total_pages"`
}

type ObjectDownloadResponse struct {
	URL string `json:"url"`
}

type ObjectUploadResponse struct {
	Created bool `json:"created"`
}

type ObjectDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

type ObjectHeadResponse struct {
	Exists bool `json:"exists"`
}

type ObjectShareResponse struct {
	URL string `json:"url"`
}
