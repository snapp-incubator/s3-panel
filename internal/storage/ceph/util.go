package ceph

import (
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	awsHttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	"github.com/ceph/go-ceph/rgw/admin"

	"github.com/snapp-incubator/S3-Panel/internal/messages"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

const (
	B = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
	PiB
)

// translateError goal is to return valid errors to user
// 401 code
// 403 code -> bucket creation quota exceed
// 404 code -> bucket not found
// 422 code -> Not processable
// 500 code -> service unavailable
func translateError(err error) storage.HTTPErrorWithCode {
	var errGenericAPI *smithy.GenericAPIError
	if errors.As(err, &errGenericAPI) {
		switch errGenericAPI.Code {
		case messages.ErrInvalidAccessKeyID:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusUnauthorized,
				Message: fmt.Errorf("invalid AccessKey"),
			}
		case messages.ErrAccessDenied:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusUnauthorized,
				Message: fmt.Errorf("access denied"),
			}
		case messages.ErrNoSuchBucket:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusNotFound,
				Message: fmt.Errorf("no such bucket"),
			}
		case messages.ErrInvalidBucketName:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusUnauthorized,
				Message: fmt.Errorf("invalid bucket name"),
			}
		case messages.ErrServiceUnavailable:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusInternalServerError,
				Message: fmt.Errorf("service unavailable"),
			}
		case messages.ErrTooManyBuckets:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusForbidden,
				Message: fmt.Errorf("bucket creation quota exceeded"),
			}
		}
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case messages.NotFound:
			return storage.HTTPErrorWithCode{
				Code:    http.StatusNotFound,
				Message: fmt.Errorf(messages.NotFound),
			}
		}
		return storage.HTTPErrorWithCode{
			Code:    http.StatusUnprocessableEntity,
			Message: err,
		}
	}

	var httpResponseErr *awsHttp.ResponseError
	if errors.As(err, &httpResponseErr) {
		return storage.HTTPErrorWithCode{
			Code:    httpResponseErr.HTTPStatusCode(),
			Message: httpResponseErr.Err,
		}
	}

	return storage.HTTPErrorWithCode{
		Code:    http.StatusUnprocessableEntity,
		Message: err,
	}
}

func NewRadosClient(endpoint, adminAccessKey, adminSecretKey string) (*admin.API, error) {
	customHTTPClient := &http.Client{
		Timeout: 60 * time.Second,
	}
	return admin.New(endpoint, adminAccessKey, adminSecretKey, admin.HTTPClient(customHTTPClient))
}

// calculateUsedBytes calculates the sum of buckets actual size from list of buckets
func calculateUsedBytes(buckets []admin.Bucket) *uint64 {
	var s uint64 = 0
	for _, bucket := range buckets {
		if bucket.Usage.RgwMain.SizeActual != nil {
			s += *bucket.Usage.RgwMain.SizeActual
		}
	}
	return &s
}

// calculateUsedObjects calculates the sum of buckets object counts from list of buckets
func calculateUsedObjects(buckets []admin.Bucket) *uint64 {
	var s uint64 = 0
	for _, bucket := range buckets {
		if bucket.Usage.RgwMain.NumObjects != nil {
			s += *bucket.Usage.RgwMain.NumObjects
		}
	}
	return &s
}

func convertSizeToUnit(sizeInBytes interface{}) (float64, string) {
	var size float64
	switch v := sizeInBytes.(type) {
	case *uint64:
		if v == nil {
			return 0, "B"
		}
		size = float64(*v)
	case *int64:
		if v == nil {
			return 0, "B"
		}
		size = float64(*v)
	case int:
		size = float64(v)
	default:
		return 0, "B"
	}

	switch {
	case size < KiB:
		return math.Round(size*100) / 100, "B"
	case size < MiB:
		return math.Round(size/KiB*100) / 100, "KiB"
	case size < GiB:
		return math.Round(size/MiB*100) / 100, "MiB"
	case size < TiB:
		return math.Round(size/GiB*100) / 100, "GiB"
	case size < PiB:
		return math.Round(size/TiB*100) / 100, "TiB"
	default:
		return math.Round(size*100) / 100, "B"
	}
}

func computeCRC32(data []byte) string {
	crc32q := crc32.MakeTable(crc32.IEEE)
	checksum := crc32.Checksum(data, crc32q)
	// S3 expects Base64-encoded checksum
	return base64.StdEncoding.EncodeToString([]byte{
		byte(checksum >> 24), byte(checksum >> 16), byte(checksum >> 8), byte(checksum),
	})
}

func parseExpiration(input string, defaultExpire time.Duration) (time.Duration, error) {
	input = strings.TrimSpace(input)
	if len(input) == 0 {
		return defaultExpire, nil
	}
	if len(input) < 2 {
		return 0, fmt.Errorf("the expiration should be at least two characters")
	}

	numStr := input[:len(input)-1]
	var unit = input[len(input)-1:]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("couldn't convert integers %v to numeric value", err)
	}

	switch unit {
	case "m", "M":
		return time.Duration(num) * time.Minute, nil
	case "h", "H":
		return time.Duration(num) * time.Hour, nil
	case "d", "D":
		return time.Duration(num) * time.Hour * 24, nil
	case "w", "W":
		return time.Duration(num) * time.Hour * 24 * 7, nil
	default:
		return 0, fmt.Errorf("invalid unit: %s", unit)
	}
}

// presignedExpiry is when a presigned SigV4 URL stops working: its X-Amz-Date
// plus X-Amz-Expires, which is what the gateway checks. Read back from the URL
// rather than computed, because the signer truncates both to whole seconds.
// fallback is returned for a URL that does not carry them.
func presignedExpiry(rawURL string, fallback time.Time) time.Time {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fallback
	}
	q := u.Query()
	signed, errDate := time.Parse("20060102T150405Z", q.Get("X-Amz-Date"))
	seconds, errExpires := strconv.ParseInt(q.Get("X-Amz-Expires"), 10, 64)
	if errDate != nil || errExpires != nil || seconds <= 0 {
		return fallback
	}
	return signed.Add(time.Duration(seconds) * time.Second)
}
