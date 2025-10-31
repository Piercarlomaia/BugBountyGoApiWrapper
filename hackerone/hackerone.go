package main

import (
	"BugBountyGoApiWrapper/env"
	"BugBountyGoApiWrapper/redismethods"
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"sync"
	"time"
)

type allProgramsResponse struct {
	Data []struct {
		Attributes struct {
			Handle string `json:"handle"`
		} `json:"attributes"`
	} `json:"data"`
}

type StructuredScope struct {
	Data []struct {
		Attributes struct {
			AssetType         string `json:"asset_type"`
			AssetIdentifier   string `json:"asset_identifier"`
			EligibleForBounty bool   `json:"eligible_for_bounty"`
		} `json:"attributes"`
	}
}

type HackeroneApi struct {
	Username string
	Token    string
	Client   *http.Client
	BaseUrl  string
}

func (api HackeroneApi) GetAllProgramsHandles() ([]string, error) {
	// Create a request with headers
	newurl := api.BaseUrl + "programs"
	req, err := http.NewRequest("GET", newurl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	var response allProgramsResponse
	req.SetBasicAuth(api.Username, api.Token)
	query := req.URL.Query()
	page := 0
	var handleslice []string
	for {
		page++
		strpage := fmt.Sprintf("%d", page)
		query.Set("page[size]", "100")
		query.Set("page[number]", strpage)
		req.URL.RawQuery = query.Encode()

		//fmt.Println("URL with parameters:", req.URL.String())

		resp, err := api.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error sending request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}
		//fmt.Println("Body content:")
		//fmt.Println(string(body))
		err = json.Unmarshal(body, &response)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling the response: %w", err)
		}

		if len(response.Data) == 0 {
			break
		}
		for _, program := range response.Data {
			handleslice = append(handleslice, program.Attributes.Handle)
			//fmt.Printf(" - %s\n", program.Attributes.Handle)
		}
	}

	//fmt.Printf("Total programs: %d\n", len(handleslice))
	//fmt.Println("Program handles slice:")
	//fmt.Println(handleslice)
	return handleslice, nil
}

func (api HackeroneApi) GetProgramStructuredScope(handle string) ([]string, error) {
	// Create a request with headers
	var response StructuredScope
	var urlsAndWildcards []string
	newurl := api.BaseUrl + "programs/" + handle + "/structured_scopes"
	req, err := http.NewRequest("GET", newurl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.SetBasicAuth(api.Username, api.Token)

	//fmt.Println("URL with parameters:", req.URL.String())

	resp, err := api.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}
	// fmt.Println("Body content:")
	// fmt.Println(string(body))
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling the response: %w", err)
	}
	for _, scope := range response.Data {
		if (scope.Attributes.AssetType == "URL" || scope.Attributes.AssetType == "WILDCARD") &&
			scope.Attributes.EligibleForBounty {
			urlsAndWildcards = append(urlsAndWildcards, scope.Attributes.AssetIdentifier)
		}
	}

	//fmt.Println("URLs and Wildcards eligible for bounty:")
	//for _, item := range urlsAndWildcards {
	//	fmt.Println(item)
	//}
	return urlsAndWildcards, nil
}

func (api HackeroneApi) GetAllUrlsProducer() ([]string, error) {
	allhandles, err := api.GetAllProgramsHandles()
	if err != nil {
		return nil, err
	}

	jobs := make(chan string, len(allhandles))
	results := make(chan []string, len(allhandles))
	errorsChan := make(chan error, len(allhandles))

	numWorkers := 10
	if len(allhandles) < numWorkers {
		numWorkers = len(allhandles)
	}

	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for handle := range jobs {
				//fmt.Printf("Worker %d processing handle: %s\n", workerID, handle)

				urls, err := api.GetProgramStructuredScope(handle)
				if err != nil {
					errorsChan <- fmt.Errorf("error fetching scope for %s: %w", handle, err)
					continue
				}
				results <- urls

				// Rate limiting: sleep between requests
				time.Sleep(1 * time.Second) // Wait 1 second between API calls
			}
		}(i)
	}

	// Send jobs to workers
	for _, handle := range allhandles {
		jobs <- handle
	}
	close(jobs)

	wg.Wait()
	close(results)
	close(errorsChan)

	if len(errorsChan) > 0 {
		return nil, fmt.Errorf("encountered errors while fetching scopes")
	}

	var allUrls []string
	for urlSlice := range results {
		allUrls = append(allUrls, urlSlice...)
	}

	fmt.Printf("Total URLs collected: %d\n", len(allUrls))
	return allUrls, nil
}

func main() {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	hackeronecli := HackeroneApi{
		env.Env["HACKERONE_USERNAME"],
		env.Env["HACKERONE_TOKEN"],
		client,
		"https://api.hackerone.com/v1/hackers/",
	}
	fmt.Println(hackeronecli.Token)

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	// Test Redis connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Redis error:", err)
		return
	}
	fmt.Println("Connected to Redis!")

	// Infinite loop that runs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Counter for tracking runs
	runCount := 0

	for {
		runCount++
		fmt.Printf("\n=== Run #%d at %s ===\n", runCount, time.Now().Format("15:04:05"))

		// Get current URLs
		currentURLs, err := hackeronecli.GetAllUrlsProducer()
		if err != nil {
			fmt.Println("Error getting URLs from HackerOne:", err)
			continue
		}

		fmt.Printf("Total URLs collected: %d\n", len(currentURLs))

		// Convert to unique set for comparison
		currentUnique := redismethods.GetUniqueURLs(currentURLs)
		fmt.Printf("Unique URLs: %d\n", len(currentUnique))

		// Try to get previous URLs from Redis
		previousUnique, err := redismethods.GetPreviousURLs(ctx, rdb)
		if err != nil {
			fmt.Println("No previous URLs found, initializing Redis with current URLs...")

			// Initialize Redis with current unique URLs
			err = redismethods.SaveURLsToRedis("hackerone:previous_urls", ctx, rdb, currentUnique)
			if err != nil {
				fmt.Println("Error initializing Redis:", err)
			} else {
				fmt.Println("Successfully initialized Redis with current URLs")
			}

			// Wait for next tick and continue
			continue
		}

		fmt.Printf("Previous unique URLs: %d\n", len(previousUnique))
		fmt.Printf("Current unique URLs: %d\n", len(currentUnique))

		// Compare unique URLs
		added, removed := redismethods.CompareUniqueURLs(previousUnique, currentUnique)

		fmt.Printf("Changes detected - New URLs: %d, Removed URLs: %d\n", len(added), len(removed))

		// Print the differences if any
		if len(added) > 0 {
			err := rdb.Publish(ctx, "telegram_notifications", fmt.Sprintf("Added: %v", added)).Err()
			if err != nil {
				fmt.Println("Error publishing to Redis:", err)
			}
			fmt.Println("\n=== NEW URLs ADDED ===")
			for i, url := range added {
				if i < 10 { // Only show first 10 to avoid spam
					fmt.Printf("%d. %s\n", i+1, url)
				}
			}
			if len(added) > 10 {
				fmt.Printf("... and %d more new URLs\n", len(added)-10)
			}
		}

		if len(removed) > 0 {
			err := rdb.Publish(ctx, "telegram_notifications", fmt.Sprintf("Removed: %v", removed)).Err()
			if err != nil {
				fmt.Println("Error publishing to Redis:", err)
			}
			fmt.Println("\n=== URLs REMOVED ===")
			for i, url := range removed {
				if i < 10 { // Only show first 10 to avoid spam
					fmt.Printf("%d. %s\n", i+1, url)
				}
			}
			if len(removed) > 10 {
				fmt.Printf("... and %d more removed URLs\n", len(removed)-10)
			}
		}

		// If no changes, print a message
		if len(added) == 0 && len(removed) == 0 {
			fmt.Println("No changes detected since last run")
		} else {
			fmt.Printf("Summary: +%d / -%d URLs\n", len(added), len(removed))
		}

		// Save current unique URLs as the new previous state
		err = redismethods.SaveURLsToRedis("hackerone:previous_urls", ctx, rdb, currentUnique)
		if err != nil {
			fmt.Println("Error saving URLs to Redis:", err)
		} else {
			fmt.Println("Current URLs saved to Redis for next comparison")
		}

		fmt.Printf("Waiting for next run at %s...\n", time.Now().Add(5*time.Minute).Format("15:04:05"))
		time.Sleep(5 * time.Minute)
	}
}
