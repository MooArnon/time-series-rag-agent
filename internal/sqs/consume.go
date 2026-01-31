package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"time-series-rag-agent/internal/database"
)

const (
	queueUrl = "https://sqs.ap-southeast-1.amazonaws.com/888577051220/trading-logs.fifo"
)

func ConsumeTradingLogs(connString string) {
	db, err := database.NewPostgresDB(connString)
	// 1. Initialize AWS Client
	cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), awsConfig.WithRegion("ap-southeast-1"))
	if err != nil {
		log.Fatalf("unable to load SDK config: %v", err)
	}
	client := sqs.NewFromConfig(cfg)

	fmt.Println("Starting SQS Consumer... (Waiting for messages)")

	for {
		// 2. Receive Message (Long Polling)
		output, err := client.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueUrl),
			MaxNumberOfMessages: 1,  // Fetch 1 at a time for simplicity
			WaitTimeSeconds:     20, // Long polling: wait up to 20s for a message
			VisibilityTimeout:   30, // 30s to process/delete before it reappears
		})

		if err != nil {
			log.Printf("failed to receive messages: %v", err)
			continue
		}

		// 3. Loop through messages (if any)
		for _, message := range output.Messages {
			fmt.Printf("Message Received! ID: %s\n", *message.MessageId)

			// 4. Parse the JSON
			var logData database.TradingLog
			err := json.Unmarshal([]byte(*message.Body), &logData)
			if err != nil {
				log.Printf("failed to unmarshal JSON: %v", err)
				continue
			}

			// --- YOUR BUSINESS LOGIC HERE ---
			fmt.Printf("Processing Signal: %s\nReason: %s\n", logData.Signal, logData.Reason)
			fmt.Printf("Processing CandleKey: %s\nCandleKey: %s\n", logData.CandleKey, logData.CandleKey)
			fmt.Printf("Processing Symbol: %s\nRecorded_at: %s\n", logData.Symbol, logData.RecordedAt)

			errIngest := db.IngestTradingLog(context.TODO(), logData)
			if errIngest != nil {
				fmt.Println("Ingestion failed: ", errIngest) // Change 'err' to 'errIngest'
				return
			}
			fmt.Println("Ingestion done")

			// e.g., Save to database or trigger an alert
			// ---------------------------------

			// 5. DELETE the message from the queue
			_, err = client.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueUrl),
				ReceiptHandle: message.ReceiptHandle, // Required for deletion
			})

			if err != nil {
				log.Printf("failed to delete message: %v", err)
			} else {
				fmt.Println("Message processed and deleted successfully.")
			}
		}
	}
}
