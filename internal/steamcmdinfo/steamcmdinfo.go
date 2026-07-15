package steamcmdinfo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const dedicatedServerAppID = "233780"

// BranchInfo is Steam branch metadata from the PICS mirror.
type BranchInfo struct {
	BuildID     string
	TimeUpdated int64
}

type appInfoResponse struct {
	Data map[string]struct {
		Depots struct {
			Branches map[string]json.RawMessage `json:"branches"`
		} `json:"depots"`
	} `json:"data"`
	Status string `json:"status"`
}

type branchRaw struct {
	BuildID     any `json:"buildid"`
	TimeUpdated any `json:"timeupdated"`
}

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func anyToInt64(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

// GetDedicatedServerBranches fetches branch build IDs for Arma 3 Dedicated Server (233780).
func GetDedicatedServerBranches() (map[string]BranchInfo, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := "https://api.steamcmd.net/v1/info/" + dedicatedServerAppID
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("steamcmd.net request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steamcmd.net returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read steamcmd.net response: %w", err)
	}

	var parsed appInfoResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse steamcmd.net response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("steamcmd.net status: %s", parsed.Status)
	}

	app, ok := parsed.Data[dedicatedServerAppID]
	if !ok {
		return nil, fmt.Errorf("steamcmd.net missing app %s", dedicatedServerAppID)
	}

	out := make(map[string]BranchInfo, len(app.Depots.Branches))
	for name, raw := range app.Depots.Branches {
		var b branchRaw
		if err := json.Unmarshal(raw, &b); err != nil {
			continue
		}
		out[name] = BranchInfo{
			BuildID:     anyToString(b.BuildID),
			TimeUpdated: anyToInt64(b.TimeUpdated),
		}
	}
	return out, nil
}

// NormalizeSteamBranch maps empty steam_branch to "public".
func NormalizeSteamBranch(steamBranch string) string {
	if steamBranch == "" {
		return "public"
	}
	return steamBranch
}
