package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/logging"
	"samrajan.me/otlbc/internal/cloud"
)

type AWSInfoLoader struct{}

func (l *AWSInfoLoader) GetInfo() (*cloud.Info, error) {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, err
	}

	cfg.Logger.Logf(logging.Debug, *identity.Account)
	return nil, nil
}
