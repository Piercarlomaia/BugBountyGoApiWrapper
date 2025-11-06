package main

import (
	"BugBountyGoApiWrapper/env"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type allProgramsResponse struct {
	Records []struct {
		Handle string `json:"handle"`
	} `json:"records"`
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

type IntigritiApi struct {
	Key     string
	Client  *http.Client
	BaseUrl string
}

func (api IntigritiApi) GetAllProgramsHandles() ([]string, error) {
	// Create a request with headers
	newurl := api.BaseUrl + "/programs"
	req, err := http.NewRequest("GET", newurl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	var response allProgramsResponse
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", api.Key))
	query := req.URL.Query()
	var handleslice []string

	query.Set("typeId", "1")
	query.Set("limit", "500")
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
	fmt.Println(string(body))
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling the response: %w", err)
	}

	for _, program := range response.Records {
		handleslice = append(handleslice, program.Handle)
		//fmt.Printf(" - %s\n", program.Attributes.Handle)
	}

	//fmt.Println(handleslice)
	return handleslice, nil
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

	intigriticli := IntigritiApi{
		env.Env["INTIGRITI_API_KEY"],
		client,
		"https://api.intigriti.com/external/researcher/v1",
	}
	fmt.Println(intigriticli.Key)

	test, err := intigriticli.GetAllProgramsHandles()
	if err != nil {
		fmt.Println("Error getting programs handles:", err)
		return
	}
	fmt.Println("Total programs:", len(test))
	fmt.Println("Program handles slice:")
	fmt.Println(test)
}
