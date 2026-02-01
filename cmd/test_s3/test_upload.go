package main

import (
	"context"
	"fmt"
	"time-series-rag-agent/internal/s3"
)

func main() {
	candleKey, _ := s3.UploadImageToS3(context.TODO(), "candle.png", "candle")
	chartKey, _ := s3.UploadImageToS3(context.TODO(), "chart.png", "chart")

	fmt.Println("candleKey: ", candleKey)
	fmt.Println("chartKey: ", chartKey)

}
