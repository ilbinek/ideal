package workshop

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// CheckUpdate checks if a mod has been updated on Steam Workshop
func CheckUpdate(workshopID string) (time.Time, error) {
	url := fmt.Sprintf("https://steamcommunity.com/sharedfiles/filedetails/?id=%s", workshopID)

	resp, err := http.Get(url)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Time{}, err
	}

	// Parse the last update time from the page
	// Look for detailsStatsContainerRight and extract the third div (Updated date)
	re := regexp.MustCompile(`<div class="detailsStatsContainerRight">[\s\S]*?<div[^>]*>([^<]+)</div>[\s\S]*?<div[^>]*>([^<]+)</div>[\s\S]*?<div[^>]*>([^<]+)</div>`)
	matches := re.FindSubmatch(body)

	var dateStr string
	if len(matches) >= 4 {
		// The third div contains the Updated date
		dateStr = strings.TrimSpace(string(matches[3]))
	} else {
		// Try alternate pattern - sometimes the order or structure varies
		re2 := regexp.MustCompile(`(?i)updated[^>]*>([^<]*<[^>]*>)*\s*([0-9]{1,2}\s+[A-Za-z]{3}[^<]*)`)
		altMatches := re2.FindSubmatch(body)
		if len(altMatches) >= 3 {
			dateStr = strings.TrimSpace(string(altMatches[2]))
		} else {
			return time.Time{}, fmt.Errorf("could not find update date on workshop page")
		}
	}

	// Try to parse various date formats Steam uses
	layouts := []string{
		"2 Jan, 2006 @ 3:04pm",
		"2 Jan, 2006 @ 3:04am",
		"2 Jan @ 3:04pm",
		"2 Jan @ 3:04am",
		"Jan 2, 2006",
	}

	var lastUpdate time.Time
	for _, layout := range layouts {
		t, err := time.Parse(layout, dateStr)
		if err == nil {
			// If year is not specified, assume current year
			if t.Year() == 0 {
				now := time.Now()
				lastUpdate = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
			} else {
				lastUpdate = t
			}
			break
		}
	}

	if lastUpdate.IsZero() {
		return time.Time{}, fmt.Errorf("could not parse update date: %s", dateStr)
	}

	return lastUpdate, nil
}

// GetModName fetches the mod name from the workshop page
func GetModName(workshopID string) (string, error) {
	url := fmt.Sprintf("https://steamcommunity.com/sharedfiles/filedetails/?id=%s", workshopID)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Parse the mod name from the page
	// Look for <div class="workshopItemTitle">MOD NAME</div>
	re := regexp.MustCompile(`<div class="workshopItemTitle">([^<]+)</div>`)
	matches := re.FindSubmatch(body)

	if len(matches) < 2 {
		return "", fmt.Errorf("could not find mod name on workshop page")
	}

	modName := strings.TrimSpace(string(matches[1]))
	return modName, nil
}
