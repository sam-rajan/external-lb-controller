package aws

import "samrajan.me/otlbc/internal/cloud"

type AWSInfoLoader struct{}

func (l *AWSInfoLoader) GetInfo() (*cloud.Info, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", "", err
	}

	return *identity.Account, cfg.Region, nil
}
