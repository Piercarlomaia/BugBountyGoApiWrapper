package redismethods

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"time"
)

type ChangeResult struct {
	Added   []string
	Removed []string
}

// getUniqueURLs converts a slice to unique set
func GetUniqueURLs(urls []string) []string {
	uniqueMap := make(map[string]bool)
	var unique []string

	for _, url := range urls {
		if !uniqueMap[url] {
			uniqueMap[url] = true
			unique = append(unique, url)
		}
	}

	return unique
}

// getPreviousURLs retrieves the previous unique URLs from Redis as JSON
func GetPreviousURLs(ctx context.Context, rdb *redis.Client) ([]string, error) {
	data, err := rdb.Get(ctx, "hackerone:previous_urls").Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("no previous URLs found")
	} else if err != nil {
		return nil, err
	}

	var urls []string
	err = json.Unmarshal([]byte(data), &urls)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling URLs from Redis: %v", err)
	}

	return urls, nil
}

// saveURLsToRedis saves the unique URLs to Redis as JSON
func SaveURLsToRedis(key string, ctx context.Context, rdb *redis.Client, urls []string) error {
	data, err := json.Marshal(urls)
	if err != nil {
		return fmt.Errorf("error marshaling URLs to JSON: %v", err)
	}

	err = rdb.Set(ctx, key, data, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("error saving to Redis: %v", err)
	}

	return nil
}

// compareUniqueURLs compares two unique URL arrays
func CompareUniqueURLs(previous, current []string) (added []string, removed []string) {
	previousMap := make(map[string]bool)
	currentMap := make(map[string]bool)

	// Create maps for O(1) lookups
	for _, url := range previous {
		previousMap[url] = true
	}
	for _, url := range current {
		currentMap[url] = true
	}

	// Find added URLs (in current but not in previous)
	for url := range currentMap {
		if !previousMap[url] {
			added = append(added, url)
		}
	}

	// Find removed URLs (in previous but not in current)
	for url := range previousMap {
		if !currentMap[url] {
			removed = append(removed, url)
		}
	}

	return added, removed
}
