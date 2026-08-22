package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/storage"
	"github.com/snapp-incubator/S3-Panel/internal/storage/ceph"
)

// HandleObjectDownload
//
//	@Summary		download an object to bucket
//	@Description	This functions downloads an object from bucket.
//	@Tags			Object
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string									true	"User given AccessKey"
//	@Param			secret_key	header		string									true	"User given SecretKey"
//	@Param			bucket		query		string									true	"bucket name"
//	@Param			object		query		string									true	"object name"
//	@Success		200			{object}	storage.ObjectDownloadResponse	"Successful response with bucket download"
//	@Failure		400			{object}	storage.OperationErrWithMsg		"Bad Request"
//	@Failure		401			{object}	string									"Unauthorized"
//	@Failure		422			{object}	storage.OperationErrWithMsg		"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg		"Internal server error"
//	@Router			/api/object/download [get]
func (s *Server) HandleObjectDownload() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.ObjectRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		// check if object exists before downloading
		exists, errObjHead := s.store.ObjectHead(s.Config.ObjectStorage, req)
		if errObjHead.Message != nil {
			s.logger.Error(errObjHead.Message.Error())
			return c.JSON(errObjHead.Code, storage.OperationErrWithMsg{Message: errObjHead.Message.Error()})
		}
		if !exists.Exists {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: "Object does not exist"})
		}

		url, errObjDownload := s.store.ObjectDownload(s.Config.ObjectStorage, req)
		if errObjDownload.Message != nil {
			s.logger.Error(errObjDownload.Message.Error())
			return c.JSON(errObjDownload.Code, storage.OperationErrWithMsg{Message: errObjDownload.Message.Error()})
		}
		return c.JSON(http.StatusOK, url)
	}
}

// HandleObjectUpload
//
//	@Summary		upload an object to bucket
//	@Description	This functions uploads an object to bucket.
//	@Tags			Object
//	@Accept			mpfd
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		formData	string								true	"bucket name"
//	@Success		200			{object}	storage.ObjectUploadResponse	"Successful response with bucket upload"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		403			{object}	storage.OperationErrWithMsg	"Forbidden"
//	@Failure		409			{object}	storage.OperationErrWithMsg	"Already Exists"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/object/upload [post]
func (s *Server) HandleObjectUpload() echo.HandlerFunc {
	formFieldBucket := "bucket"
	var maxUploadSize int64 = 1024 * 1024 * 1024

	return func(c echo.Context) error {
		var req storage.ObjectUploadRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		req.Bucket = c.FormValue(formFieldBucket)
		if req.Bucket == "" {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: "bucket can not be empty"})
		}

		req.Prefix = c.FormValue("prefix")

		form, errForm := c.MultipartForm()
		if errForm != nil {
			s.logger.Error(errForm.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: errForm.Error()})
		}

		files := form.File["files"]

		hasLargeFile := false
		for _, file := range files {
			// do not allow uploading files larger than 1G
			if file.Size > maxUploadSize {
				hasLargeFile = true
				break
			}

			// check if object exists before upload
			headReq := storage.ObjectRequestMeta{
				AccessKey: req.AccessKey,
				SecretKey: req.SecretKey,
				Bucket:    req.Bucket,
				Object:    req.Prefix + file.Filename,
			}
			existOut, existsErr := s.store.ObjectHead(s.Config.ObjectStorage, headReq)
			if existsErr.Message != nil {
				s.logger.Error(existsErr.Message.Error())
				return c.JSON(existsErr.Code, storage.OperationErrWithMsg{Message: existsErr.Message.Error()})
			} else if existOut.Exists {
				return c.JSON(http.StatusConflict, storage.OperationErrWithMsg{Message: fmt.Sprintf("File %s already exists", file.Filename)})
			}
		}

		if hasLargeFile {
			return c.JSON(http.StatusForbidden, storage.OperationErrWithMsg{Message: "files larger than 1G are not allowed to upload"})
		}

		for _, file := range files {
			_, errObjectUpload := s.store.ObjectUpload(s.Config.ObjectStorage, req, file)
			if errObjectUpload.Message != nil {
				s.logger.Error(errObjectUpload.Message.Error())
				return c.JSON(errObjectUpload.Code, storage.OperationErrWithMsg{Message: errObjectUpload.Message.Error()})
			}
			s.logger.Debug(fmt.Sprintf("File %s Uploaded to bucket %s of user %s Successfully", file.Filename, req.Bucket, req.AccessKey))
		}
		return c.JSON(http.StatusOK, storage.ObjectUploadResponse{Created: true})
	}
}

// HandleObjectList
//
//	@Summary		List of objects of a bucket
//	@Description	Fetches list of buckets owned by a user that is specified by AccessKey and SecretKey
//	@Tags			Object
//	@Accept			json
//	@Produce		json
//	@Param			access_key		header		string								true	"User given AccessKey"
//	@Param			secret_key		header		string								true	"User given SecretKey"
//	@Param			bucket			query		string								true	"bucket name"
//	@Param			max_keys		query		string								true	"max_keys of pagination"
//	@Param			page			query		string								true	"page of pagination"
//	@Param			search_string	query		string								true	"search by given string, could be empty"
//	@Success		200				{object}	storage.ObjectListResponse	"Successful response with bucket list"
//	@Failure		400				{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401				{object}	string								"Unauthorized"
//	@Failure		422				{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500				{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/object/list [get]
func (s *Server) HandleObjectList() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.ObjectListRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		// In iam mode this handler cannot take the admin path below, and does not
		// need to.
		//
		// It cannot: the panel holds no RGW admin credential there — IAM does —
		// so building an admin client fails outright ("access key not set") and
		// the listing 422s before it ever reaches the gateway.
		//
		// It does not need to: the admin client exists only to resolve a uid and
		// to confirm the bucket is one the caller owns. In iam mode the control
		// endpoint has already decided the caller may read this bucket, and the
		// authorization middleware has already rewritten it to the gateway's
		// spelling. Re-deriving ownership from RGW would additionally get the
		// WRONG answer: the caller's credential is a session under a tenant
		// account, so "buckets this account owns" is not "buckets this person may
		// see".
		if s.Config.Server.IsIAMMode() {
			objects, errObjectList := s.store.ObjectList(s.Config.ObjectStorage, req)
			if errObjectList.Message != nil {
				s.logger.Error(errObjectList.Message.Error())
				return c.JSON(errObjectList.Code, storage.OperationErrWithMsg{Message: errObjectList.Message.Error()})
			}
			objects.TotalPages = totalPages(objects.TotalMatchedItems, int(req.MaxKeys))
			return c.JSON(http.StatusOK, objects)
		}

		radosClient, err := ceph.NewRadosClient(s.Config.ObjectStorage.URL, s.Config.ObjectStorage.AccessKeyAdmin, s.Config.ObjectStorage.SecretKeyAdmin)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: err.Error()})
		}

		userID, _, errGetUser := findUserID(s, radosClient, req.AccessKey)
		if errGetUser != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: errGetUser.Error()})
		}

		reqList := storage.BucketListAndQuotaRequestMeta{
			AccessKey: req.AccessKey,
			SecretKey: req.SecretKey,
			MaxKeys:   1000,
			Page:      1,
			UID:       userID,
		}
		outputBucketList, errBucketList := s.store.BucketList(s.Config.ObjectStorage, reqList)
		if errBucketList.Message != nil {
			return c.JSON(errBucketList.Code, storage.OperationErrWithMsg{Message: errBucketList.Message.Error()})
		}
		outputBucketQuota, errBucketQuota := s.store.BucketQuota(s.Config.ObjectStorage, reqList, outputBucketList)
		if errBucketQuota.Message != nil {
			return c.JSON(errBucketQuota.Code, storage.OperationErrWithMsg{Message: errBucketQuota.Message.Error()})
		}

		objects, errObjectList := s.store.ObjectList(s.Config.ObjectStorage, req)
		if errObjectList.Message != nil {
			s.logger.Error(errObjectList.Message.Error())
			return c.JSON(errObjectList.Code, storage.OperationErrWithMsg{Message: errObjectList.Message.Error()})
		}

		var bucketFound = false
		for _, bucketData := range outputBucketQuota.Items {
			if bucketData.BucketName == req.Bucket {
				if objects.TotalMatchedItems%int(req.MaxKeys) == 0 {
					objects.TotalPages = objects.TotalMatchedItems / int(req.MaxKeys)
				} else {
					objects.TotalPages = (objects.TotalMatchedItems / int(req.MaxKeys)) + 1
				}
				bucketFound = true
			}
		}

		if !bucketFound {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: "Bucket Not Found"})
		}

		return c.JSON(http.StatusOK, objects)
	}
}

// HandleObjectsDelete
//
//	@Summary		Deletes the list of objects inside a bucket
//	@Description	Deletes a list of objects specified by name
//	@Tags			Object
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		query		string								true	"bucket name"
//	@Param			objects		query		[]string							true	"objects names"
//	@Success		200			{object}	storage.ObjectDeleteResponse	"Successful response with objects delete"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		403			{object}	storage.OperationErrWithMsg	"Object Does not exist"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/object/delete [delete]
func (s *Server) HandleObjectsDelete() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.ObjectDeleteRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		for _, obj := range req.Objects {
			// check if object exists before deleting
			headReq := storage.ObjectRequestMeta{
				AccessKey: req.AccessKey,
				SecretKey: req.SecretKey,
				Bucket:    req.Bucket,
				Object:    obj,
			}
			existOut, existsErr := s.store.ObjectHead(s.Config.ObjectStorage, headReq)
			if existsErr.Message != nil {
				s.logger.Error(existsErr.Message.Error())
				return c.JSON(existsErr.Code, storage.OperationErrWithMsg{Message: existsErr.Message.Error()})
			} else if !existOut.Exists {
				return c.JSON(http.StatusForbidden, storage.OperationErrWithMsg{Message: fmt.Sprintf("File %s does not exist", obj)})
			}
		}

		objects, errObjectDelete := s.store.ObjectsDelete(s.Config.ObjectStorage, req)
		if errObjectDelete.Message != nil {
			s.logger.Error(errObjectDelete.Message.Error())
			return c.JSON(errObjectDelete.Code, storage.OperationErrWithMsg{Message: errObjectDelete.Message.Error()})
		}
		return c.JSON(http.StatusOK, objects)
	}
}

// HandleObjectHead
//
//	@Summary		check if an object exists
//	@Description	check if an object exists
//	@Tags			Object
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		body		string								true	"bucket name"
//	@Param			object		body		string								true	"objects name"
//	@Success		200			{object}	storage.ObjectHeadResponse	"Successful response with objects head"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/object/head [get]
func (s *Server) HandleObjectHead() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.ObjectRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		objects, errObjectHead := s.store.ObjectHead(s.Config.ObjectStorage, req)
		if errObjectHead.Message != nil {
			s.logger.Error(errObjectHead.Message.Error())
			return c.JSON(errObjectHead.Code, storage.OperationErrWithMsg{Message: errObjectHead.Message.Error()})
		}
		return c.JSON(http.StatusOK, objects)
	}
}

// HandleObjectShare
//
//	@Summary		share the preSign address of object
//	@Description	This function uses the PreSign feature of S3 to share object link with expiration time with users
//	@Tags			Object
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		body		string								true	"bucket name"
//	@Param			object		body		string								true	"objects name"
//	@Param			expiration	body		string								false	"URL expiration time"	default(1h)
//	@Success		200			{object}	storage.ObjectShareResponse	"Successful response with object url"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/object/share [get]
func (s *Server) HandleObjectShare() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.ObjectRequestMeta
		err := c.Bind(&req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = (&echo.DefaultBinder{}).BindHeaders(c, &req)
		if err != nil {
			s.logger.Error(err.Error())
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		err = c.Validate(req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: err.Error()})
		}

		exists, errObjHead := s.store.ObjectHead(s.Config.ObjectStorage, req)
		if errObjHead.Message != nil {
			s.logger.Error(errObjHead.Message.Error())
			return c.JSON(errObjHead.Code, storage.OperationErrWithMsg{Message: errObjHead.Message.Error()})
		}
		if !exists.Exists {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: "Object does not exist"})
		}

		url, errObjShare := s.store.ObjectShare(s.Config.ObjectStorage, req)
		if errObjShare.Message != nil {
			s.logger.Error(errObjShare.Message.Error())
			return c.JSON(errObjShare.Code, storage.OperationErrWithMsg{Message: errObjShare.Message.Error()})
		}
		return c.JSON(http.StatusOK, url)
	}
}
