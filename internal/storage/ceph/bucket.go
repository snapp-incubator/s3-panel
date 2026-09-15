package ceph

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/messages"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

func (c CephObjectStorage) BucketCreate(serverAdminConfig config.ObjectStorageConfig, meta storage.BucketActionRequestMeta) (storage.BucketCreateResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.BucketCreateResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	_, errCreate := client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(meta.Bucket),
	})
	if errCreate != nil {
		return storage.BucketCreateResponse{}, translateError(errCreate)
	}

	errHeadBucket := s3.NewBucketExistsWaiter(client).Wait(context.Background(), &s3.HeadBucketInput{Bucket: aws.String(meta.Bucket)}, time.Minute)
	if errHeadBucket != nil {
		return storage.BucketCreateResponse{}, translateError(errHeadBucket)
	}

	return storage.BucketCreateResponse{
		Created: true,
	}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) BucketDelete(serverAdminConfig config.ObjectStorageConfig, meta storage.BucketActionRequestMeta) (storage.BucketDeleteResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.BucketDeleteResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}
	_, errDelete := client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(meta.Bucket)})
	if errDelete != nil {
		return storage.BucketDeleteResponse{}, translateError(errDelete)
	}

	err = s3.NewBucketNotExistsWaiter(client).Wait(context.Background(), &s3.HeadBucketInput{Bucket: aws.String(meta.Bucket)}, time.Minute)
	if err != nil {
		return storage.BucketDeleteResponse{}, translateError(err)
	}
	return storage.BucketDeleteResponse{
		Deleted:    true,
		HasObjects: false,
	}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) BucketList(serverAdminConfig config.ObjectStorageConfig, meta storage.BucketListAndQuotaRequestMeta) (storage.BucketListResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.BucketListResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	if meta.MaxKeys <= 0 {
		meta.MaxKeys = 10
	}
	if meta.Page <= 0 {
		meta.Page = 1
	}
	downThreshold := int(meta.MaxKeys) * (int(meta.Page) - 1)
	upThreshold := int(meta.MaxKeys) * (int(meta.Page))

	var buckets []string
	var totalItems int
	bucketPaginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{})
	for bucketPaginator.HasMorePages() {
		output, errNextPage := bucketPaginator.NextPage(context.Background())
		if errNextPage != nil {
			return storage.BucketListResponse{}, translateError(errNextPage)
		} else {
			for _, bucket := range output.Buckets {
				if meta.SearchString != "" && !strings.Contains(strings.ToLower(*bucket.Name), strings.ToLower(meta.SearchString)) {
					continue
				}
				totalItems += 1
				if downThreshold < totalItems && totalItems <= upThreshold {
					buckets = append(buckets, *bucket.Name)
				}
			}
		}
	}
	return storage.BucketListResponse{
		Items:        buckets,
		TotalBuckets: totalItems,
	}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) BucketQuota(serverAdminConfig config.ObjectStorageConfig, meta storage.BucketListAndQuotaRequestMeta, matchedBuckets storage.BucketListResponse) (storage.BucketQuotaResponse, storage.HTTPErrorWithCode) {
	radosClient, err := NewRadosClient(serverAdminConfig.URL, serverAdminConfig.AccessKeyAdmin, serverAdminConfig.SecretKeyAdmin)
	if err != nil {
		return storage.BucketQuotaResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	bucketsData, errBuckets := radosClient.ListUsersBucketsWithStat(context.Background(), meta.UID)
	if errBuckets != nil {
		return storage.BucketQuotaResponse{}, translateError(errBuckets)
	}

	var aggregatedBucketData []storage.SingleBucketQuotaResponse
	for _, bucketData := range bucketsData {
		bucketFound := false
		// as we check the MatchString in the list method, and we call it on the handler, we will only check for the buckets from the BucketList Output
		for _, matchedBucket := range matchedBuckets.Items {
			if matchedBucket == bucketData.Bucket {
				bucketFound = true
				break
			}
		}
		if !bucketFound {
			continue
		}

		usedByteValue, usedByteUnit := convertSizeToUnit(bucketData.Usage.RgwMain.SizeActual)
		hardByteValue, hardByteUnit := convertSizeToUnit(bucketData.BucketQuota.MaxSize)
		var usedObjects int
		if bucketData.Usage.RgwMain.NumObjects == nil {
			usedObjects = 0
		} else {
			usedObjects = int(*bucketData.Usage.RgwMain.NumObjects)
		}
		bucketQuotaInfo := storage.SingleBucketQuotaResponse{
			BucketName:      bucketData.Bucket,
			QuotaEnabled:    bucketData.BucketQuota.Enabled,
			UsedBytes:       usedByteValue,
			UsedBytesUnit:   usedByteUnit,
			UsedBytesRaw:    bucketData.Usage.RgwMain.SizeActual,
			HardBytes:       hardByteValue,
			HardBytesUnit:   hardByteUnit,
			HardBytesRaw:    bucketData.BucketQuota.MaxSize,
			UsedObjects:     usedObjects,
			HardObjects:     bucketData.BucketQuota.MaxObjects,
			ModifyTimeStamp: bucketData.Mtime,
			Tenant:          bucketData.Tenant,
			Access:          "R/W",
		}
		aggregatedBucketData = append(aggregatedBucketData, bucketQuotaInfo)
	}

	return storage.BucketQuotaResponse{Items: aggregatedBucketData, TotalBuckets: matchedBuckets.TotalBuckets, TotalPages: matchedBuckets.TotalPages}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}
