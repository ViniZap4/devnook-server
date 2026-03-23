package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// In-memory per-user rate limiting for link preview requests
var (
	linkRateMu  sync.Mutex
	linkRateMap = make(map[int64]time.Time)
)

type linkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	Domain      string `json:"domain"`
}

// Regexes for extracting OG/meta tags and <title>. Each pair covers both
// attribute orderings (property= before content= and vice versa).
var (
	reOGTitle     = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	reOGTitleAlt  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:title["']`)
	reOGDesc      = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']+)["']`)
	reOGDescAlt   = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:description["']`)
	reOGImage     = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`)
	reOGImageAlt  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:image["']`)
	reMetaDesc    = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']`)
	reMetaDescAlt = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+name=["']description["']`)
	reTitleTag    = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
)

func firstMatch(re1, re2 *regexp.Regexp, s string) string {
	if m := re1.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if m := re2.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func isPrivateIP(host string) bool {
	ips, err := net.LookupIP(host)
	if err != nil {
		return true // deny on resolution failure
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
	}
	return false
}

func (h *Handler) GetLinkPreview(c *fiber.Ctx) error {
	claims := getClaims(c)
	rawURL := c.Query("url")
	if rawURL == "" {
		return writeError(c, fiber.StatusBadRequest, "url parameter is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return writeError(c, fiber.StatusBadRequest, "invalid URL")
	}

	// SSRF protection: reject private IPs
	hostname := parsed.Hostname()
	if isPrivateIP(hostname) {
		return writeError(c, fiber.StatusForbidden, "private URLs are not allowed")
	}

	// Rate limit: 1 request per second per user
	linkRateMu.Lock()
	last, exists := linkRateMap[claims.UserID]
	now := time.Now()
	if exists && now.Sub(last) < time.Second {
		linkRateMu.Unlock()
		return writeError(c, fiber.StatusTooManyRequests, "rate limited")
	}
	linkRateMap[claims.UserID] = now
	linkRateMu.Unlock()

	ctx := context.Background()

	// Check cache (24h TTL)
	var cached linkPreview
	err = h.db.QueryRow(ctx,
		`SELECT url, title, description, image_url, domain FROM link_previews
		 WHERE url = $1 AND fetched_at > NOW() - INTERVAL '24 hours'`, rawURL,
	).Scan(&cached.URL, &cached.Title, &cached.Description, &cached.ImageURL, &cached.Domain)
	if err == nil {
		return writeJSON(c, fiber.StatusOK, cached)
	}

	// Fetch the URL — with SSRF protection on redirects
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if isPrivateIP(req.URL.Hostname()) {
				return fmt.Errorf("redirect to private IP blocked")
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid URL")
	}
	req.Header.Set("User-Agent", "DevNook LinkPreview/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return writeError(c, fiber.StatusBadGateway, "failed to fetch URL")
	}
	defer resp.Body.Close()

	// Limit read to 1MB
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return writeError(c, fiber.StatusBadGateway, "failed to read response")
	}

	// Parse OG tags
	preview := linkPreview{
		URL:    rawURL,
		Domain: parsed.Hostname(),
	}
	parseOGTags(string(bodyBytes), &preview)

	// Upsert into cache
	if _, err := h.db.Exec(ctx,
		`INSERT INTO link_previews (url, title, description, image_url, domain)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (url) DO UPDATE SET title=$2, description=$3, image_url=$4, domain=$5, fetched_at=NOW()`,
		preview.URL, preview.Title, preview.Description, preview.ImageURL, preview.Domain); err != nil {
		log.Printf("failed to cache link preview for %s: %v", rawURL, err)
	}

	return writeJSON(c, fiber.StatusOK, preview)
}

func parseOGTags(body string, preview *linkPreview) {
	// Truncate to <head> section to avoid scanning the whole document
	if idx := strings.Index(strings.ToLower(body), "<body"); idx != -1 {
		body = body[:idx]
	}

	preview.Title = firstMatch(reOGTitle, reOGTitleAlt, body)
	preview.Description = firstMatch(reOGDesc, reOGDescAlt, body)
	preview.ImageURL = firstMatch(reOGImage, reOGImageAlt, body)

	if preview.Description == "" {
		preview.Description = firstMatch(reMetaDesc, reMetaDescAlt, body)
	}

	if preview.Title == "" {
		if m := reTitleTag.FindStringSubmatch(body); len(m) > 1 {
			preview.Title = strings.TrimSpace(m[1])
		}
	}
}
