package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type DiscordClient struct {
	OrderWebhookURL    string
	PipelineWebhookURL string
	Client             *http.Client
}

// NewDiscordClient sets up the webhook sender
func NewDiscordClient(orderURL, pipelineURL string) *DiscordClient {
	return &DiscordClient{
		OrderWebhookURL:    orderURL,
		PipelineWebhookURL: pipelineURL,
		Client:             &http.Client{Timeout: 10 * time.Second},
	}
}

// NotifyOrder sends to the Order Room
func (d *DiscordClient) NotifyOrder(msg string, imagePath string) {
	d.send(d.OrderWebhookURL, "**TRADE ALERT**\n"+msg, imagePath)
}

// NotifyPipeline sends to the Pipeline Room
func (d *DiscordClient) NotifyPipeline(msg string, imagePath string) {
	d.send(d.PipelineWebhookURL, "**Pipeline...**\n"+msg, imagePath)
}

// send handles the Logic: Text Only vs Text + Image
func (d *DiscordClient) send(webhookURL, content string, imagePath string) {
	if webhookURL == "" {
		return
	}

	// 1. If NO Image, send simple JSON
	if imagePath == "" {
		d.sendSimpleText(webhookURL, content)
		return
	}

	// 2. If Image Exists, send Multipart Request
	err := d.sendMultipart(webhookURL, content, imagePath)
	if err != nil {
		log.Printf("⚠️ Webhook Image Failed (%s): %v. Fallback to text.", imagePath, err)
		d.sendSimpleText(webhookURL, content) // Fallback
	}
}

// sendSimpleText sends a lightweight JSON payload
func (d *DiscordClient) sendSimpleText(url, content string) {
	payload := map[string]string{"content": content}
	jsonBody, _ := json.Marshal(payload)

	resp, err := d.Client.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("⚠️ Webhook Error: %v", err)
		return
	}
	defer resp.Body.Close()
}

// sendMultipart handles File Upload + Content
func (d *DiscordClient) sendMultipart(url, content, path string) error {
	// Open file
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Prepare Body
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// A. Add the File field ("file" is Discord's requirement)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}

	// B. Add the Text field ("content")
	// Note: We use WriteField for the text caption
	_ = writer.WriteField("content", content)

	// Close writer to finalize boundary
	err = writer.Close()
	if err != nil {
		return err
	}

	// C. Send Request
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}

	// CRITICAL: Set the Content-Type with the boundary
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	return nil
}
