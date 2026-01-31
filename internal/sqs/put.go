package sqs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func PutTradingLog(messageBody string) {
	// 1. Generate the dynamic names you requested
	now := time.Now()
	// 3. Initialize AWS Client
	ctx := context.TODO()
	cfg, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion("ap-southeast-1"))
	if err != nil {
		log.Fatalf("unable to load SDK config: %v", err)
	}
	client := sqs.NewFromConfig(cfg)

	queueUrl := "https://sqs.ap-southeast-1.amazonaws.com/888577051220/trading-logs.fifo"

	// 4. Send Message with FIFO Parameters
	output, err := client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueUrl),
		MessageBody: aws.String(messageBody),
		// REQUIRED for FIFO: Ensures logs are processed in order
		MessageGroupId: aws.String("trading-bot-logs"),
		// RECOMMENDED for FIFO: Prevents duplicate messages if retrying within 5 mins
		MessageDeduplicationId: aws.String(fmt.Sprintf("log_%d", now.UnixNano())),
	})

	if err != nil {
		log.Fatalf("failed to send message to FIFO queue: %v", err)
	}

	fmt.Printf("Message Sent! ID: %s\nPayload: %s\n", *output.MessageId, messageBody)
}
