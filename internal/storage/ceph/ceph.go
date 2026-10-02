package ceph

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

type CephObjectStorage struct{}

func NewCephObjectStorage() storage.ObjectStorage {
	return CephObjectStorage{}
}

// NewClient builds an S3 client for one set of credentials.
//
// sessionToken is what makes a short-lived credential usable: an STS credential
// is only honoured when the request also carries X-Amz-Security-Token, which the
// SDK sends when — and only when — aws.Credentials.SessionToken is set. Dropping
// it here makes every call in iam mode fail with InvalidAccessKeyId against a
// genuinely STS-backed gateway. It is empty for the long-lived keys a user types
// in s3 mode, and the SDK then omits the header.
func (c CephObjectStorage) NewClient(endpoint, accessKey, secretKey, sessionToken string) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
				SessionToken:    sessionToken,
			}, nil
		})),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %v", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	}), nil
}

func (c CephObjectStorage) NewPreSignClient(endpoint, accessKey, secretKey, sessionToken string, expiration time.Duration) (*s3.PresignClient, error) {
	client, err := c.NewClient(endpoint, accessKey, secretKey, sessionToken)
	if err != nil {
		return nil, err
	}

	return s3.NewPresignClient(client, func(options *s3.PresignOptions) {
		options.Expires = expiration
	}), nil
}
