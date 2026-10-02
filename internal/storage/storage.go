package storage

import (
	"mime/multipart"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

type HTTPErrorWithCode struct {
	Code    int
	Message error
}

type ObjectStorage interface {
	NewClient(endpoint, accessKey, secretKey, sessionToken string) (*s3.Client, error)

	ObjectsDelete(cfg config.ObjectStorageConfig, meta ObjectDeleteRequestMeta) (ObjectDeleteResponse, HTTPErrorWithCode)
	ObjectDownload(cfg config.ObjectStorageConfig, meta ObjectRequestMeta) (ObjectDownloadResponse, HTTPErrorWithCode)
	ObjectList(cfg config.ObjectStorageConfig, meta ObjectListRequestMeta) (ObjectListResponse, HTTPErrorWithCode)
	ObjectUpload(cfg config.ObjectStorageConfig, meta ObjectUploadRequestMeta, files *multipart.FileHeader) (ObjectUploadResponse, HTTPErrorWithCode)
	ObjectHead(cfg config.ObjectStorageConfig, meta ObjectRequestMeta) (ObjectHeadResponse, HTTPErrorWithCode)
	ObjectShare(cfg config.ObjectStorageConfig, meta ObjectRequestMeta) (ObjectShareResponse, HTTPErrorWithCode)

	BucketCreate(cfg config.ObjectStorageConfig, meta BucketActionRequestMeta) (BucketCreateResponse, HTTPErrorWithCode)
	BucketDelete(cfg config.ObjectStorageConfig, meta BucketActionRequestMeta) (BucketDeleteResponse, HTTPErrorWithCode)
	BucketList(cfg config.ObjectStorageConfig, meta BucketListAndQuotaRequestMeta) (BucketListResponse, HTTPErrorWithCode)
	BucketQuota(cfg config.ObjectStorageConfig, meta BucketListAndQuotaRequestMeta, bucketList BucketListResponse) (BucketQuotaResponse, HTTPErrorWithCode)

	UserQuota(cfg config.ObjectStorageConfig, meta UserRequestMeta) (UserQuotaResponse, HTTPErrorWithCode)
	UserIdentification(cfg config.ObjectStorageConfig, meta UserRequestMeta) (UserIdentificationResponse, HTTPErrorWithCode)
}
