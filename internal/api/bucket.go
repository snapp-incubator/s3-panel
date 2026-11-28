package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/messages"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
	"github.com/snapp-incubator/S3-Panel/internal/storage/ceph"
)

// HandleBucketList
//
//	@Summary		List of buckets of a user
//	@Description	Fetches list of buckets owned by a user that is specified by AccessKey and SecretKey
//	@Tags			Bucket
//	@Accept			json
//	@Produce		json
//	@Param			access_key		header		string								true	"User given AccessKey"
//	@Param			secret_key		header		string								true	"User given SecretKey"
//	@Param			search_string	query		string								false	"search by given string, could be empty"
//	@Param			max_keys		query		string								false	"max keys in a page"
//	@Param			page			query		string								false	"page number"
//	@Success		200				{object}	storage.BucketListResponse	"Successful response with bucket list"
//	@Failure		400				{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401				{object}	string								"Unauthorized"
//	@Failure		422				{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500				{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/bucket/list [get]
func (s *Server) HandleBucketList() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.BucketListAndQuotaRequestMeta
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

		radosClient, err := ceph.NewRadosClient(s.Config.ObjectStorage.URL, s.Config.ObjectStorage.AccessKeyAdmin, s.Config.ObjectStorage.SecretKeyAdmin)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: err.Error()})
		}

		userID, _, errGetUser := findUserID(s, radosClient, req.AccessKey)
		if errGetUser != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: errGetUser.Error()})
		}
		req.UID = userID

		buckets, errBucketList := s.store.BucketList(s.Config.ObjectStorage, req)
		if errBucketList.Message != nil {
			s.logger.Error(errBucketList.Message.Error())
			return c.JSON(errBucketList.Code, storage.OperationErrWithMsg{Message: errBucketList.Message.Error()})
		}

		if buckets.TotalBuckets%int(req.MaxKeys) == 0 {
			buckets.TotalPages = buckets.TotalBuckets / int(req.MaxKeys)
		} else {
			buckets.TotalPages = (buckets.TotalBuckets / int(req.MaxKeys)) + 1
		}

		return c.JSON(http.StatusOK, buckets)
	}
}

// HandleBucketQuota
//
//	@Summary		Quota of buckets of a user
//	@Description	Fetches Quota of buckets owned by a user that is specified by AccessKey. if status code is 200 and the BucketQuotaResponse hard_bytes or hard_objects is -1, mean no limitation on quota_storage or quota_objects is applied.
//	@Tags			Bucket
//	@Accept			json
//	@Produce		json
//	@Param			access_key		header		string								true	"User given AccessKey"
//	@Param			secret_key		header		string								true	"User given SecretKey"
//	@Param			search_string	query		string								false	"search by given string, could be empty"
//	@Param			max_keys		query		string								false	"max keys in a page"
//	@Param			page			query		string								false	"page number"
//	@Success		200				{object}	storage.BucketQuotaResponse	"Successful response with buckets quota"
//	@Failure		400				{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401				{object}	string								"Unauthorized"
//	@Failure		422				{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500				{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/bucket/quota [get]
func (s *Server) HandleBucketQuota() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.BucketListAndQuotaRequestMeta
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

		radosClient, err := ceph.NewRadosClient(s.Config.ObjectStorage.URL, s.Config.ObjectStorage.AccessKeyAdmin, s.Config.ObjectStorage.SecretKeyAdmin)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: err.Error()})
		}

		userID, _, errGetUser := findUserID(s, radosClient, req.AccessKey)
		if errGetUser != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: errGetUser.Error()})
		}
		req.UID = userID

		// first we try to get a BucketList to make sure the provided credentials are correct
		bucketList, errBucketList := s.store.BucketList(s.Config.ObjectStorage, req)
		if errBucketList.Message != nil {
			s.logger.Error(errBucketList.Message.Error())
			return c.JSON(errBucketList.Code, storage.OperationErrWithMsg{Message: errBucketList.Message.Error()})
		}

		if bucketList.TotalBuckets%int(req.MaxKeys) == 0 {
			bucketList.TotalPages = bucketList.TotalBuckets / int(req.MaxKeys)
		} else {
			bucketList.TotalPages = (bucketList.TotalBuckets / int(req.MaxKeys)) + 1
		}

		quotaInfo, errBucketQuota := s.store.BucketQuota(s.Config.ObjectStorage, req, bucketList)
		if errBucketQuota.Message != nil {
			s.logger.Error(errBucketQuota.Message.Error())
			return c.JSON(errBucketQuota.Code, storage.OperationErrWithMsg{Message: errBucketQuota.Message.Error()})
		}
		return c.JSON(http.StatusOK, quotaInfo)
	}
}

// HandleBucketCreate
//
//	@Summary		Creates Bucket
//	@Description	Creates bucket for a user
//	@Tags			Bucket
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		body		string								true	"Bucket Name to Create"
//	@Success		201			{object}	storage.BucketCreateResponse	"Bucket created successfully"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		403			{object}	storage.OperationErrWithMsg	"Bucket Already Exists / Bucket creation quota exceeded"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/bucket/create [post]
func (s *Server) HandleBucketCreate(c echo.Context) error {
	var req storage.BucketActionRequestMeta
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

	bucketListInput := storage.BucketListAndQuotaRequestMeta{
		AccessKey: req.AccessKey,
		SecretKey: req.SecretKey,
		MaxKeys:   1000,
		Page:      1,
	}
	bucketList, errBucketList := s.store.BucketList(s.Config.ObjectStorage, bucketListInput)
	if errBucketList.Message != nil {
		return c.JSON(errBucketList.Code, storage.OperationErrWithMsg{Message: errBucketList.Message.Error()})
	}
	for _, bucket := range bucketList.Items {
		if bucket == req.Bucket {
			return c.JSON(http.StatusForbidden, storage.OperationErrWithMsg{Message: messages.ErrBucketAlreadyExists})
		}
	}

	createdBucket, errBucketCreate := s.store.BucketCreate(s.Config.ObjectStorage, req)
	if errBucketCreate.Message != nil {
		s.logger.Error(errBucketCreate.Message.Error())
		return c.JSON(errBucketCreate.Code, storage.OperationErrWithMsg{Message: errBucketCreate.Message.Error()})
	}
	return c.JSON(http.StatusCreated, createdBucket)
}

// HandleBucketDelete
//
//	@Summary		Deletes Bucket
//	@Description	Deletes bucket for a user
//	@Tags			Bucket
//	@Accept			json
//	@Produce		json
//	@Param			access_key	header		string								true	"User given AccessKey"
//	@Param			secret_key	header		string								true	"User given SecretKey"
//	@Param			bucket		body		string								true	"Bucket Name to Delete"
//	@Success		200			{object}	storage.BucketDeleteResponse	"Bucket deleted successfully"
//	@Failure		400			{object}	storage.OperationErrWithMsg	"Bad Request"
//	@Failure		401			{object}	string								"Unauthorized"
//	@Failure		422			{object}	storage.OperationErrWithMsg	"Action didn't complete"
//	@Failure		500			{object}	storage.OperationErrWithMsg	"Internal server error"
//	@Router			/api/bucket/delete [delete]
func (s *Server) HandleBucketDelete() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req storage.BucketActionRequestMeta
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

		radosClient, err := ceph.NewRadosClient(s.Config.ObjectStorage.URL, s.Config.ObjectStorage.AccessKeyAdmin, s.Config.ObjectStorage.SecretKeyAdmin)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: err.Error()})
		}

		userID, _, errGetUser := findUserID(s, radosClient, req.AccessKey)
		if errGetUser != nil {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: errGetUser.Error()})
		}

		reqBucketQuota := storage.BucketListAndQuotaRequestMeta{
			AccessKey: req.AccessKey,
			SecretKey: req.SecretKey,
			UID:       userID,
			MaxKeys:   1000,
			Page:      1,
		}
		bucketList, bucketListQuotaErr := s.store.BucketList(s.Config.ObjectStorage, reqBucketQuota)
		if bucketListQuotaErr.Message != nil {
			return c.JSON(bucketListQuotaErr.Code, bucketListQuotaErr.Message.Error())
		}

		bucketFound := false
		for _, bucket := range bucketList.Items {
			if bucket == req.Bucket {
				bucketFound = true
				limitedBucketList := storage.BucketListResponse{TotalBuckets: 1, TotalPages: 1, Items: []string{bucket}}
				bucketQuota, bucketQuotaErr := s.store.BucketQuota(s.Config.ObjectStorage, reqBucketQuota, limitedBucketList)
				if bucketQuotaErr.Message != nil {
					return c.JSON(bucketQuotaErr.Code, bucketQuotaErr.Message.Error())
				}
				if bucketQuota.Items[0].UsedObjects != 0 {
					return c.JSON(http.StatusForbidden, "Bucket is not empty for deletion")
				}
				break
			}
		}

		if !bucketFound {
			return c.JSON(http.StatusUnprocessableEntity, storage.OperationErrWithMsg{Message: messages.ErrNoSuchBucket})
		}

		deleteBucket, errBucketDelete := s.store.BucketDelete(s.Config.ObjectStorage, req)
		if errBucketDelete.Message != nil {
			s.logger.Error(errBucketDelete.Message.Error())
			return c.JSON(errBucketDelete.Code, storage.OperationErrWithMsg{Message: errBucketDelete.Message.Error()})
		}
		return c.JSON(http.StatusOK, deleteBucket)
	}
}
