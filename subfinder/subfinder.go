package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/publicsuffix"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	for {
		// Connect to Redis
		ctx := context.Background()
		rdb := redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})

		// Test Redis connection
		_, err := rdb.Ping(ctx).Result()
		if err != nil {
			log.Fatalf("Failed to connect to Redis: %v", err)
		}
		fmt.Println("Connected to Redis!")

		// Get URLs from Redis
		urlsData, err := rdb.Get(ctx, "hackerone:previous_urls").Result()
		if err != nil {
			log.Fatalf("Failed to get URLs from Redis: %v", err)
		}

		// Parse the URLs data as JSON array
		var urls []string
		err = json.Unmarshal([]byte(urlsData), &urls)
		if err != nil {
			log.Fatalf("Failed to unmarshal URLs from Redis: %v", err)
		}
		// Extract unique domains from URLs
		domainMap := make(map[string]bool)
		for _, urlStr := range urls {
			domain := extractDomain(urlStr)
			//fmt.Printf("Extracted domain: %s\n", domain)
			//fmt.Printf("Extracted urlstr: %s\n", urlStr)
			if domain != "" {
				domainMap[domain] = true
			}
		}

		domains := make([]string, 0, len(domainMap))
		for domain := range domainMap {
			domains = append(domains, domain)
		}

		fmt.Printf("Found %d unique domains from Redis\n", len(domains))

		// Query Anubis for each domain with rate limiting
		client := &http.Client{Timeout: 30 * time.Second}

		// Rate limiting: 1 request every 500ms (2 requests per second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// Collect all subdomains in a flat list
		allSubdomains := make(map[string]bool)

		for i, domain := range domains {
			<-ticker.C // Wait for the next tick

			fmt.Printf("[%d/%d] Querying Anubis for: %s\n", i+1, len(domains), domain)

			subdomains, err := queryAnubis(client, domain)
			if err != nil {
				log.Printf("Failed to query Anubis for %s: %v", domain, err)
				continue
			}

			fmt.Printf("Found %d subdomains for %s\n", len(subdomains), domain)

			// Add the main domain itself
			allSubdomains[domain] = true

			// Add all subdomains
			for _, subdomain := range subdomains {
				allSubdomains[subdomain] = true
			}
		}

		// Convert map to slice
		currentSubdomains := make([]string, 0, len(allSubdomains))
		for subdomain := range allSubdomains {
			currentSubdomains = append(currentSubdomains, subdomain)
		}

		fmt.Printf("\nTotal unique subdomains found: %d\n", len(currentSubdomains))

		// Compare with previous run
		compareAndSave(ctx, rdb, currentSubdomains)

		// Wait 60 minutes before next run
		fmt.Printf("Waiting 60 minutes until next run...\n")
		time.Sleep(60 * time.Minute)
	}
}

// queryAnubis queries the Anubis API for subdomains
func queryAnubis(client *http.Client, domain string) ([]string, error) {
	anubisURL := fmt.Sprintf("https://anubisdb.com/anubis/subdomains/%s", domain)

	req, err := http.NewRequest("GET", anubisURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SecurityScanner/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anubis API returned status: %d", resp.StatusCode)
	}

	var subdomains []string
	err = json.NewDecoder(resp.Body).Decode(&subdomains)
	if err != nil {
		return nil, err
	}

	return subdomains, nil
}

func compareAndSave(ctx context.Context, rdb *redis.Client, current []string) {
	// Get previous subdomains
	previousData, err := rdb.Get(ctx, "anubis:previous_subdomains").Result()

	var previous []string
	if err == nil {
		json.Unmarshal([]byte(previousData), &previous)
	}

	// Convert to sets for comparison
	currentSet := make(map[string]bool)
	previousSet := make(map[string]bool)

	for _, sub := range current {
		currentSet[sub] = true
	}
	for _, sub := range previous {
		previousSet[sub] = true
	}

	// Find differences
	added := make([]string, 0)
	removed := make([]string, 0)

	for sub := range currentSet {
		if !previousSet[sub] {
			added = append(added, sub)
		}
	}

	for sub := range previousSet {
		if !currentSet[sub] {
			removed = append(removed, sub)
		}
	}

	// Print results
	fmt.Printf("\n=== CHANGES DETECTED ===\n")
	fmt.Printf("Added: %d subdomains\n", len(added))
	fmt.Printf("Removed: %d subdomains\n", len(removed))

	// Send notification if changes detected
	if len(added) > 0 || len(removed) > 0 {
		// Send summary first
		summary := fmt.Sprintf("Subdomain Changes Detected\nAdded: %d | Removed: %d", len(added), len(removed))
		err = rdb.Publish(ctx, "telegram_notifications", summary).Err()
		if err != nil {
			log.Printf("Error publishing summary notification: %v", err)
		} else {
			fmt.Println("Summary sent to Telegram")
		}

		// Send added subdomains in chunks of 10
		if len(added) > 0 {
			sendInChunks(ctx, rdb, added, "🆕 Added Subdomains", 30)
		}

		// Send removed subdomains in chunks of 10
		if len(removed) > 0 {
			//sendInChunks(ctx, rdb, removed, "🗑️ Removed Subdomains", 30)
		}

		fmt.Println("All notifications sent to Telegram")
	} else {
		fmt.Println("No changes detected since last run")
	}

	// Save current as new previous
	currentData, _ := json.Marshal(current)
	err = rdb.Set(ctx, "anubis:previous_subdomains", currentData, 7*24*time.Hour).Err()
	if err != nil {
		log.Printf("Error saving to Redis: %v", err)
	} else {
		fmt.Println("Results saved for next comparison")
	}
}

// sendInChunks sends domains in chunks of specified size
func sendInChunks(ctx context.Context, rdb *redis.Client, domains []string, title string, chunkSize int) {
	totalDomains := len(domains)
	totalChunks := (totalDomains + chunkSize - 1) / chunkSize // Ceiling division

	fmt.Printf("Sending %d %s in %d chunks...\n", totalDomains, strings.ToLower(title), totalChunks)

	for i := 0; i < totalDomains; i += chunkSize {
		end := i + chunkSize
		if end > totalDomains {
			end = totalDomains
		}

		chunk := domains[i:end]
		chunkNumber := (i / chunkSize) + 1

		// Format the message
		message := formatChunkMessage(chunk, title, chunkNumber, totalChunks)

		// Send to Telegram
		err := rdb.Publish(ctx, "telegram_notifications", message).Err()
		if err != nil {
			log.Printf("Error publishing %s chunk %d: %v", title, chunkNumber, err)
		} else {
			fmt.Printf("✓ Sent %s - Part %d/%d: %d domains\n",
				strings.ToLower(title), chunkNumber, totalChunks, len(chunk))
		}

		// Small delay to avoid rate limiting
		time.Sleep(3100 * time.Millisecond)
	}
}

// formatChunkMessage creates a formatted message for a chunk of domains
func formatChunkMessage(domains []string, title string, chunkNum, totalChunks int) string {
	domainsText := strings.Join(domains, "\n")

	return fmt.Sprintf("%s (%d/%d) - %d domains:\n%s",
		title, chunkNum, totalChunks, len(domains), domainsText)
}

// extractDomain extracts domain from URL or wildcard
// extractDomain extracts only the main domain from URL or wildcard
func extractDomain(input string) string {
	// Use publicsuffix to get the main domain first
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.SplitN(input, "/", 2)[0]

	domain, err := publicsuffix.EffectiveTLDPlusOne(input)
	if err != nil {
		// If publicsuffix fails, fallback to cleaning the input
		domain = input
	}

	return domain
}
