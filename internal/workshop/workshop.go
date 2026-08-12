package workshop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SteamAPIResponse represents the response from Steam Web API
type SteamAPIResponse struct {
	Response struct {
		PublishedFileDetails []struct {
			Result          int    `json:"result"`
			PublishedFileID string `json:"publishedfileid"`
			Title           string `json:"title"`
			TimeUpdated     int64  `json:"time_updated"`
		} `json:"publishedfiledetails"`
	} `json:"response"`
}

// CheckUpdate checks if a mod has been updated on Steam Workshop using the API
func CheckUpdate(workshopID, apiKey string) (time.Time, error) {
	details, err := GetModDetails([]string{workshopID}, apiKey)
	if err != nil {
		return time.Time{}, err
	}

	if len(details) == 0 {
		return time.Time{}, fmt.Errorf("no details returned for workshop ID %s", workshopID)
	}

	if details[0].Result != 1 {
		return time.Time{}, fmt.Errorf("failed to get mod details, result code: %d", details[0].Result)
	}

	return time.Unix(details[0].TimeUpdated, 0), nil
}

// GetModName fetches the mod name from the Steam API
func GetModName(workshopID, apiKey string) (string, error) {
	details, err := GetModDetails([]string{workshopID}, apiKey)
	if err != nil {
		return "", err
	}

	if len(details) == 0 {
		return "", fmt.Errorf("no details returned for workshop ID %s", workshopID)
	}

	if details[0].Result != 1 {
		return "", fmt.Errorf("failed to get mod details, result code: %d", details[0].Result)
	}

	return details[0].Title, nil
}

// GetModDetails fetches details for multiple mods from Steam Web API
func GetModDetails(workshopIDs []string, apiKey string) ([]struct {
	Result          int    `json:"result"`
	PublishedFileID string `json:"publishedfileid"`
	Title           string `json:"title"`
	TimeUpdated     int64  `json:"time_updated"`
}, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Steam API key is not configured")
	}

	// Build the API URL
	baseURL := "https://api.steampowered.com/IPublishedFileService/GetDetails/v1/"
	params := url.Values{}
	params.Add("key", apiKey)

	for i, id := range workshopIDs {
		params.Add(fmt.Sprintf("publishedfileids[%d]", i), id)
	}

	fullURL := baseURL + "?" + params.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to call Steam API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Steam API returned status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read API response: %w", err)
	}

	var apiResponse SteamAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return apiResponse.Response.PublishedFileDetails, nil
}
