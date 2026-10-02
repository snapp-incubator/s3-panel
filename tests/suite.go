package tests

import (
	"context"
	"strings"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/snapp-incubator/S3-Panel/internal/api"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/logging"
)

var conf config.Config
var confPath = "./../configs/test-config.toml"

func init() {
	cfg := config.Provide(confPath)
	conf = cfg
}

type HttpMessageError struct {
	Message string `json:"message"`
}

type BaseTestSuite struct {
	suite.Suite
	server  *api.Server
	context context.Context
	cancel  context.CancelFunc
	*require.Assertions
}

func (s *BaseTestSuite) SetupSuite() {
	// This suite drives a real server against a real object-storage backend, so
	// it cannot run without one. configs/test-config.toml ships with no endpoint,
	// which made `go test ./...` fail for everyone with an unhelpful
	// "failed to setup application" — CI only stayed green because it excludes
	// this package by name (`go list ./... | grep -v /tests`).
	//
	// Skipping when nothing is configured says the same thing honestly, keeps a
	// plain `go test ./...` green, and lets the suite actually run for anyone who
	// points the config at a gateway. Set object_storage_config.url (and its
	// keys) to a reachable S3 endpoint to exercise it.
	if strings.TrimSpace(conf.ObjectStorage.URL) == "" {
		s.T().Skip("object_storage_config.url is not set in " + confPath +
			" — this suite needs a reachable S3 endpoint")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.context = ctx
	s.cancel = cancel
	s.Assertions = s.Require()
	testServer, err := api.NewServer(ctx, cancel, conf, logging.Provide(conf.Logger))
	if err != nil {
		s.FailNowf("failed to setup application with err : ", err.Error())
	}

	s.server = testServer
	go func() {
		errStart := testServer.Start()
		if errStart != nil {
			s.FailNow("Failed to start router", errStart.Error())
		}
	}()
}

func (s *BaseTestSuite) TearDownSuite() {
	s.cancel()

	if err := s.server.ShutDown(); err != nil {
		s.FailNowf("failed to close server with err : ", err.Error())
	}
}
