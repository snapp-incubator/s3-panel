package ceph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/messages"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

const DefaultPreSignShareExpiration = time.Hour * 1
const PreSignDownloadExpiration = time.Minute * 1

func (c CephObjectStorage) ObjectsDelete(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectDeleteRequestMeta) (storage.ObjectDeleteResponse, storage.HTTPErrorWithCode) {
	if len(meta.Objects) == 0 {
		return storage.ObjectDeleteResponse{}, storage.HTTPErrorWithCode{Code: http.StatusBadRequest, Message: fmt.Errorf("you should specify at least one object")}
	}

	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.ObjectDeleteResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	var objects []types.ObjectIdentifier
	for _, obj := range meta.Objects {
		objects = append(objects, types.ObjectIdentifier{Key: aws.String(obj)})
	}
	input := s3.DeleteObjectsInput{
		Bucket: aws.String(meta.Bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	}

	deleteOut, errDelete := client.DeleteObjects(context.Background(), &input)
	if errDelete != nil {
		return storage.ObjectDeleteResponse{}, translateError(errDelete)
	} else if len(deleteOut.Errors) > 0 {
		return storage.ObjectDeleteResponse{}, translateError(errors.New(*deleteOut.Errors[0].Message))
	}

	for _, delObj := range deleteOut.Deleted {
		err = s3.NewObjectNotExistsWaiter(client).Wait(context.Background(), &s3.HeadObjectInput{Bucket: aws.String(meta.Bucket), Key: delObj.Key}, time.Minute)
		if err != nil {
			return storage.ObjectDeleteResponse{}, translateError(err)
		}
	}

	return storage.ObjectDeleteResponse{Deleted: true}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) ObjectDownload(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectRequestMeta) (storage.ObjectDownloadResponse, storage.HTTPErrorWithCode) {
	expiration := PreSignDownloadExpiration

	preSignClient, err := c.NewPreSignClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken, expiration)
	if err != nil {
		return storage.ObjectDownloadResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	objectDownloadInput := s3.GetObjectInput{
		Bucket: aws.String(meta.Bucket),
		Key:    aws.String(meta.Object),
	}
	urlPreSign, errPreSignGet := preSignClient.PresignGetObject(context.Background(), &objectDownloadInput)
	if errPreSignGet != nil {
		return storage.ObjectDownloadResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errPreSignGet}
	}

	return storage.ObjectDownloadResponse{URL: urlPreSign.URL}, storage.HTTPErrorWithCode{}
}

// Bounds on what one listing request may cost the gateway.
const (
	// maxListPageSize is the most entries ListObjectsV2 returns in one call.
	maxListPageSize = 1000
	// maxPagesSkipped caps how far a caller paging by number, without a
	// continuation token, may jump: each page skipped is one gateway call.
	maxPagesSkipped = 100
	// searchScanPage is how many keys a search asks the gateway for at a time,
	// and searchScanBudget how many it may examine in one request.
	searchScanPage   = 1000
	searchScanBudget = 5000
)

func (c CephObjectStorage) ObjectList(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectListRequestMeta) (storage.ObjectListResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.ObjectListResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	if meta.MaxKeys <= 0 {
		meta.MaxKeys = 10
	}
	if meta.MaxKeys > maxListPageSize {
		meta.MaxKeys = maxListPageSize
	}
	if meta.Page <= 0 {
		meta.Page = 1
	}

	if strings.TrimSpace(meta.SearchString) != "" {
		return c.objectListSearch(client, meta)
	}
	return c.objectListByPrefix(client, meta)
}

// objectListByPrefix returns one page of a folder — its subfolders and files —
// from a single ListObjectsV2 call.
//
// It used to list the whole folder on every request, 500 keys at a time, only
// to count the entries and slice one page out of them. A folder holding millions
// of objects meant thousands of gateway calls per click, and the page never
// rendered. Resuming from the gateway's continuation token makes a page cost one
// call, whatever the bucket holds.
func (c CephObjectStorage) objectListByPrefix(client *s3.Client, meta storage.ObjectListRequestMeta) (storage.ObjectListResponse, storage.HTTPErrorWithCode) {
	token := meta.ContinuationToken

	// A caller paging by number, with no token, is walked to its page one call
	// per page skipped — bounded by the page number, not by the bucket's size.
	skip := 0
	if token == "" {
		skip = int(meta.Page) - 1
	}
	if skip > maxPagesSkipped {
		return storage.ObjectListResponse{}, storage.HTTPErrorWithCode{
			Code:    http.StatusBadRequest,
			Message: fmt.Errorf("page %d is too deep to reach by number; follow next_continuation_token instead", meta.Page),
		}
	}

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:    aws.String(meta.Bucket),
			MaxKeys:   aws.Int32(meta.MaxKeys),
			Delimiter: aws.String("/"),
		}
		if meta.Prefix != "" {
			input.Prefix = aws.String(meta.Prefix)
		}
		if token != "" {
			input.ContinuationToken = aws.String(token)
		}

		output, errList := client.ListObjectsV2(context.Background(), input)
		if errList != nil {
			return storage.ObjectListResponse{}, translateError(errList)
		}

		truncated := aws.ToBool(output.IsTruncated)
		next := aws.ToString(output.NextContinuationToken)

		if skip > 0 {
			if !truncated || next == "" {
				// The listing ends before the page asked for.
				return storage.ObjectListResponse{
					Items:      []storage.ObjectListBody{},
					TotalPages: int(meta.Page) - skip,
				}, storage.HTTPErrorWithCode{}
			}
			skip--
			token = next
			continue
		}

		items := make([]storage.ObjectListBody, 0, len(output.CommonPrefixes)+len(output.Contents))
		for _, cp := range output.CommonPrefixes {
			name := strings.TrimPrefix(aws.ToString(cp.Prefix), meta.Prefix)
			items = append(items, storage.ObjectListBody{Name: &name, IsFolder: true})
		}
		for _, obj := range output.Contents {
			name := strings.TrimPrefix(aws.ToString(obj.Key), meta.Prefix)
			// The folder's own placeholder object, not a file inside it.
			if name == "" {
				continue
			}
			items = append(items, fileEntry(name, obj))
		}

		return pageResponse(meta, items, truncated, next), storage.HTTPErrorWithCode{}
	}
}

// objectListSearch finds the keys under the current folder whose name below it
// contains the search string.
//
// S3 has no substring search, so this has to look at keys — and it used to look
// at every key in the bucket on every request, which on a large bucket never
// finished. Now one request examines at most searchScanBudget keys: it stops
// when the page is full or the budget is spent and hands back the last key it
// examined to resume from. A page can therefore come back short, or empty, with
// more of the bucket still to search.
func (c CephObjectStorage) objectListSearch(client *s3.Client, meta storage.ObjectListRequestMeta) (storage.ObjectListResponse, storage.HTTPErrorWithCode) {
	needle := strings.ToLower(strings.TrimSpace(meta.SearchString))

	// The token is the last key the previous page examined, resumed with
	// StartAfter rather than the gateway's continuation token: a search can stop
	// part-way through a gateway page once its own page is full, and a
	// continuation token could only resume after the whole of it.
	after := meta.ContinuationToken
	items := make([]storage.ObjectListBody, 0, meta.MaxKeys)
	scanned := 0

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(meta.Bucket),
			MaxKeys: aws.Int32(int32(min(searchScanPage, searchScanBudget-scanned))),
		}
		if meta.Prefix != "" {
			input.Prefix = aws.String(meta.Prefix)
		}
		if after != "" {
			input.StartAfter = aws.String(after)
		}

		output, errList := client.ListObjectsV2(context.Background(), input)
		if errList != nil {
			return storage.ObjectListResponse{}, translateError(errList)
		}
		truncated := aws.ToBool(output.IsTruncated)

		for i, obj := range output.Contents {
			key := aws.ToString(obj.Key)
			after = key
			scanned++

			if strings.Contains(strings.ToLower(strings.TrimPrefix(key, meta.Prefix)), needle) {
				items = append(items, fileEntry(key, obj))
			}
			if len(items) == int(meta.MaxKeys) {
				more := i < len(output.Contents)-1 || truncated
				return pageResponse(meta, items, more, after), storage.HTTPErrorWithCode{}
			}
		}

		if !truncated || len(output.Contents) == 0 {
			return pageResponse(meta, items, false, ""), storage.HTTPErrorWithCode{}
		}
		if scanned >= searchScanBudget {
			return pageResponse(meta, items, true, after), storage.HTTPErrorWithCode{}
		}
	}
}

// fileEntry is one object as the listing reports it, under the given name.
func fileEntry(name string, obj types.Object) storage.ObjectListBody {
	sizeValue, sizeUnit := convertSizeToUnit(obj.Size)
	return storage.ObjectListBody{
		Name:                  &name,
		SizeValue:             sizeValue,
		SizeUnit:              sizeUnit,
		LastModifiedTimestamp: obj.LastModified.String(),
		IsFolder:              false,
	}
}

// pageResponse assembles one page. A listing is only reported as continuing
// when there is a token to continue it with.
func pageResponse(meta storage.ObjectListRequestMeta, items []storage.ObjectListBody, more bool, next string) storage.ObjectListResponse {
	more = more && next != ""
	pages := int(meta.Page)
	if more {
		pages++
	} else {
		next = ""
	}
	return storage.ObjectListResponse{
		Items:                 items,
		TotalMatchedItems:     len(items),
		TotalPages:            pages,
		IsTruncated:           more,
		NextContinuationToken: next,
	}
}

func (c CephObjectStorage) ObjectUpload(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectUploadRequestMeta, file *multipart.FileHeader) (storage.ObjectUploadResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.ObjectUploadResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	objectKey := meta.Prefix + file.Filename

	uploadMultiPartInput := &s3.CreateMultipartUploadInput{
		Bucket:            aws.String(meta.Bucket),
		Key:               aws.String(objectKey),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
	}
	respMP, errCreateMP := client.CreateMultipartUpload(context.Background(), uploadMultiPartInput)
	if errCreateMP != nil {
		return storage.ObjectUploadResponse{}, translateError(errCreateMP)
	}

	src, errOpen := file.Open()
	if errOpen != nil {
		return storage.ObjectUploadResponse{}, translateError(errOpen)
	}
	defer func() { _ = src.Close() }()

	bs := make([]byte, file.Size)
	if _, errReadBuf := io.ReadFull(src, bs); errReadBuf != nil {
		return storage.ObjectUploadResponse{}, translateError(errReadBuf)
	}

	const maxPartSize = int64(6 * 1024 * 1024)
	var curr, partLength int64
	var remaining = file.Size
	var completedParts []types.CompletedPart
	var partNumber int32 = 1
	for curr = 0; remaining != 0; curr += partLength {
		partNum := partNumber
		if remaining < maxPartSize {
			partLength = remaining
		} else {
			partLength = maxPartSize
		}

		checkSumCRC32 := computeCRC32(bs[curr : curr+partLength])
		partInput := &s3.UploadPartInput{
			Body:              bytes.NewReader(bs[curr : curr+partLength]),
			Bucket:            aws.String(meta.Bucket),
			Key:               aws.String(objectKey),
			PartNumber:        &partNum,
			UploadId:          respMP.UploadId,
			ChecksumCRC32:     &checkSumCRC32,
			ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
		}
		uploadResult, errUploadPart := client.UploadPart(context.TODO(), partInput)
		if errUploadPart != nil {
			aboInput := &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(meta.Bucket),
				Key:      aws.String(objectKey),
				UploadId: respMP.UploadId,
			}
			_, aboErr := client.AbortMultipartUpload(context.TODO(), aboInput)
			if aboErr != nil {
				return storage.ObjectUploadResponse{}, translateError(aboErr)
			}
			return storage.ObjectUploadResponse{}, translateError(errUploadPart)
		}

		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadResult.ETag,
			PartNumber: &partNum,
		})
		remaining -= partLength
		partNumber += 1
	}

	compInput := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(meta.Bucket),
		Key:      aws.String(objectKey),
		UploadId: respMP.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
		ChecksumType: types.ChecksumTypeComposite,
	}
	_, compErr := client.CompleteMultipartUpload(context.Background(), compInput)
	if compErr != nil {
		aboInput := &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(meta.Bucket),
			Key:      aws.String(objectKey),
			UploadId: respMP.UploadId,
		}
		_, errAbort := client.AbortMultipartUpload(context.Background(), aboInput)
		if errAbort != nil {
			return storage.ObjectUploadResponse{}, translateError(errAbort)
		}
		return storage.ObjectUploadResponse{}, translateError(compErr)
	}

	return storage.ObjectUploadResponse{
		Created: true,
	}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) ObjectHead(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectRequestMeta) (storage.ObjectHeadResponse, storage.HTTPErrorWithCode) {
	client, err := c.NewClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken)
	if err != nil {
		return storage.ObjectHeadResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	headObjectInput := s3.HeadObjectInput{
		Bucket: aws.String(meta.Bucket),
		Key:    aws.String(meta.Object),
	}
	_, headObjectError := client.HeadObject(context.Background(), &headObjectInput)

	if headObjectError != nil {
		httpErrorCode := translateError(headObjectError)
		if httpErrorCode.Message.Error() == messages.NotFound {
			return storage.ObjectHeadResponse{Exists: false}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
		}
		return storage.ObjectHeadResponse{}, httpErrorCode
	}

	return storage.ObjectHeadResponse{Exists: true}, storage.HTTPErrorWithCode{Code: 0, Message: nil}
}

func (c CephObjectStorage) ObjectShare(serverAdminConfig config.ObjectStorageConfig, meta storage.ObjectRequestMeta) (storage.ObjectShareResponse, storage.HTTPErrorWithCode) {
	expiration, errConvert := parseExpiration(meta.Expiration, DefaultPreSignShareExpiration)
	if errConvert != nil {
		return storage.ObjectShareResponse{}, storage.HTTPErrorWithCode{Code: http.StatusUnprocessableEntity, Message: errConvert}
	}
	// A link cannot outlive the credential that signed it. Without this a caller
	// asking for 24h — accepted, there is no upper bound — receives a URL that
	// silently starts answering 403 as soon as the short-lived credential behind
	// it lapses, which in iam mode is within the hour.
	if meta.MaxExpiration > 0 && expiration > meta.MaxExpiration {
		expiration = meta.MaxExpiration
	}

	preSignClient, err := c.NewPreSignClient(serverAdminConfig.URL, meta.AccessKey, meta.SecretKey, meta.SessionToken, expiration)
	if err != nil {
		return storage.ObjectShareResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errors.New(messages.FailedToCreateClient)}
	}

	objectShareInput := s3.GetObjectInput{
		Bucket: aws.String(meta.Bucket),
		Key:    aws.String(meta.Object),
	}
	urlPreSign, errPreSignGet := preSignClient.PresignGetObject(context.Background(), &objectShareInput)
	if errPreSignGet != nil {
		return storage.ObjectShareResponse{}, storage.HTTPErrorWithCode{Code: http.StatusInternalServerError, Message: errPreSignGet}
	}

	return storage.ObjectShareResponse{URL: urlPreSign.URL}, storage.HTTPErrorWithCode{}
}
