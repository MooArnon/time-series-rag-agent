package s3

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	bucket = "vector-quant-trader-log"
)

// UploadImageToS3 takes a local file path and uploads it with a dynamic timestamp name
func UploadImageToS3(ctx context.Context, localFilePath string) (string, error) {
	key := GetS3Path()

	// 1. Initialize AWS Config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to load SDK config: %v", err)
	}
	client := s3.NewFromConfig(cfg)

	// 3. Open the local file
	file, err := os.Open(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %q: %v", localFilePath, err)
	}
	defer file.Close()

	// 4. Upload to S3
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String("image/png"),
	})

	if err == nil {
		fmt.Printf("Successfully uploaded to: s3://%s/%s\n", bucket, key)
	}

	return key, err
}

func GetS3Path() (key string) {
	now := time.Now()

	// 2. Format the prefix: image/candle/YYYY/MM/DD/
	// Note: We strip "s3://" as the SDK expects the path starting from the root of the bucket
	prefix := now.Format("image/candle/2006/01/02/")

	// 3. Format the filename: YYYYMMDD_HHMMSS.png
	fileName := now.Format("20060102_150405.png")

	// 4. Combine for the full S3 Key
	key = prefix + fileName

	return key
}
