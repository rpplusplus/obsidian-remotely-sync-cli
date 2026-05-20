package main

import (
	"context"
	"fmt"

	"obsidian-remotely-sync-cli/config"
	"obsidian-remotely-sync-cli/s3"
)

// connectS3 creates an S3 client from config.
func connectS3(ctx context.Context, cfg *config.Config) (*s3.Client, error) {
	client, err := s3.NewClient(ctx,
		cfg.S3.Endpoint,
		cfg.S3.Region,
		cfg.S3.AccessKey,
		cfg.S3.SecretKey,
		cfg.S3.Bucket,
		cfg.S3.Prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to S3: %w", err)
	}
	return client, nil
}
