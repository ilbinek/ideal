package parser

import (
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type ModInfo struct {
	WorkshopID string
	Name       string
}

// ParseModsetHTML parses an Arma 3 launcher modset HTML file
func ParseModsetHTML(reader io.Reader) (string, []ModInfo, error) {
	doc, err := html.Parse(reader)
	if err != nil {
		return "", nil, err
	}

	var modsetName string
	var mods []ModInfo

	// Find modset name from <meta name="arma:PresetName" content="...">
	var findMeta func(*html.Node)
	findMeta = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var name, content string
			for _, attr := range n.Attr {
				if attr.Key == "name" {
					name = attr.Val
				}
				if attr.Key == "content" {
					content = attr.Val
				}
			}
			if name == "arma:PresetName" {
				modsetName = content
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findMeta(c)
		}
	}
	findMeta(doc)

	// Find all mod entries
	var findMods func(*html.Node)
	findMods = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			// Check if this is a ModContainer
			for _, attr := range n.Attr {
				if attr.Key == "data-type" && attr.Val == "ModContainer" {
					mod := parseModRow(n)
					if mod.WorkshopID != "" {
						mods = append(mods, mod)
					}
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findMods(c)
		}
	}
	findMods(doc)

	return modsetName, mods, nil
}

func parseModRow(n *html.Node) ModInfo {
	var mod ModInfo

	// Find td elements
	var findTDs func(*html.Node)
	findTDs = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "td" {
			// Check for DisplayName
			for _, attr := range node.Attr {
				if attr.Key == "data-type" && attr.Val == "DisplayName" {
					mod.Name = getTextContent(node)
					break
				}
			}

			// Look for link with workshop ID
			var findLink func(*html.Node)
			findLink = func(linkNode *html.Node) {
				if linkNode.Type == html.ElementNode && linkNode.Data == "a" {
					for _, attr := range linkNode.Attr {
						if attr.Key == "href" && strings.Contains(attr.Val, "steamcommunity.com/sharedfiles/filedetails/?id=") {
							// Extract workshop ID from URL
							re := regexp.MustCompile(`id=(\d+)`)
							matches := re.FindStringSubmatch(attr.Val)
							if len(matches) > 1 {
								mod.WorkshopID = matches[1]
							}
							break
						}
					}
				}
				for c := linkNode.FirstChild; c != nil; c = c.NextSibling {
					findLink(c)
				}
			}
			findLink(node)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			findTDs(c)
		}
	}
	findTDs(n)

	return mod
}

func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data)
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += getTextContent(c)
	}
	return strings.TrimSpace(text)
}
