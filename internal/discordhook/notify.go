package discordhook

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ilbinek/godishook"
)

const username = "IDEAL"

// NotifyModUpdate sends a Discord embed for a Workshop mod update used by servers.
func NotifyModUpdate(webhookURL, modName, workshopID string, serverNames []string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	client, err := godishook.NewFromURL(webhookURL)
	if err != nil {
		return fmt.Errorf("discord webhook: %w", err)
	}

	servers := strings.Join(serverNames, ", ")
	if servers == "" {
		servers = "(none)"
	}
	workshopURL := fmt.Sprintf("https://steamcommunity.com/sharedfiles/filedetails/?id=%s", workshopID)

	_, err = client.Send(context.Background(), godishook.Message().
		Username(username).
		Embed(godishook.NewEmbed().
			Title("Mod update available").
			URL(workshopURL).
			Color(0xE67E22).
			Timestamp(time.Now()).
			Field("Mod", modName, false).
			Field("Workshop ID", workshopID, true).
			Field("Servers", servers, false).
			Footer("IDEAL mod manager", "")))
	if err != nil {
		return err
	}
	log.Printf("[DISCORD] notified mod update: %s (%s)", modName, workshopID)
	return nil
}

// NotifyServerEvent sends a Discord embed about something that happened to a server.
// Unlike the update notifications it takes a context, since it's sent while a server
// is down and a hung webhook mustn't linger.
func NotifyServerEvent(ctx context.Context, webhookURL, title, serverName, detail string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	client, err := godishook.NewFromURL(webhookURL)
	if err != nil {
		return fmt.Errorf("discord webhook: %w", err)
	}
	embed := godishook.NewEmbed().
		Title(title).
		Color(0xE74C3C).
		Timestamp(time.Now()).
		Field("Server", serverName, false).
		Footer("IDEAL mod manager", "")
	if strings.TrimSpace(detail) != "" {
		embed = embed.Field("Details", detail, false)
	}
	if _, err := client.Send(ctx, godishook.Message().Username(username).Embed(embed)); err != nil {
		return err
	}
	log.Printf("[DISCORD] notified %q: %s", title, serverName)
	return nil
}

// NotifyBranchUpdate sends a Discord embed for a dedicated-server branch update.
func NotifyBranchUpdate(webhookURL, branchName, steamBranch, remoteBuildID string, serverNames []string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	client, err := godishook.NewFromURL(webhookURL)
	if err != nil {
		return fmt.Errorf("discord webhook: %w", err)
	}

	servers := strings.Join(serverNames, ", ")
	if servers == "" {
		servers = "(none)"
	}
	steamLabel := steamBranch
	if steamLabel == "" {
		steamLabel = "public"
	}

	_, err = client.Send(context.Background(), godishook.Message().
		Username(username).
		Embed(godishook.NewEmbed().
			Title("Server branch update available").
			Color(0x3498DB).
			Timestamp(time.Now()).
			Field("Branch", branchName, true).
			Field("Steam branch", steamLabel, true).
			Field("Remote build", remoteBuildID, true).
			Field("Servers", servers, false).
			Footer("IDEAL mod manager", "")))
	if err != nil {
		return err
	}
	log.Printf("[DISCORD] notified branch update: %s (build %s)", branchName, remoteBuildID)
	return nil
}
