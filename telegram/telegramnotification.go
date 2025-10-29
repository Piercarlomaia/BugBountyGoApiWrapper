package main

import (
	"BugBountyGoApiWrapper/env"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {
	botToken := env.Env["TELEGRAM_BOT_TOKEN"]
	chatID := env.Env["TELEGRAM_CHAT_ID"]
	redisChannel := "telegram_notifications"

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()

	pubsub := rdb.Subscribe(ctx, redisChannel)
	defer pubsub.Close()

	_, err := pubsub.Receive(ctx)
	if err != nil {
		log.Fatal("Error subscribing to channel:", err)
	}

	log.Printf("Listening for messages on channel: %s", redisChannel)

	ch := pubsub.Channel()
	var wg sync.WaitGroup

	for msg := range ch {
		wg.Add(1)
		go func(message string) {
			defer wg.Done()

			// Send to Telegram with retry
			maxRetries := 3
			retryDelay := time.Second

			for attempt := 1; attempt <= maxRetries; attempt++ {
				err := sendToTelegram(botToken, chatID, message)
				if err == nil {
					log.Printf("Message sent successfully (attempt %d)", attempt)
					return
				}

				log.Printf("Attempt %d failed: %v", attempt, err)

				if attempt < maxRetries {
					log.Printf("Retrying in %v...", retryDelay)
					time.Sleep(retryDelay)
					retryDelay *= 2 // Exponential backoff
				}
			}

			log.Printf("Failed to send message after %d attempts: %s", maxRetries, message)
		}(msg.Payload)
	}

	wg.Wait() // This will never be reached in this case, but good practice
}

func sendToTelegram(botToken, chatID, text string) error {
	payload := map[string]string{
		"chat_id": chatID,
		"text":    text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: %s - %s", resp.Status, string(body))
	}

	return nil
}
