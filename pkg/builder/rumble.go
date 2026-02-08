package builder

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const rumbleBaseURL = "https://rumble.com"

type RumbleBuilder struct {
	client *http.Client
}

func NewRumbleBuilder() (*RumbleBuilder, error) {
	return &RumbleBuilder{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (b *RumbleBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse URL")
	}

	var pageURL string
	switch info.LinkType {
	case model.TypeChannel:
		pageURL = fmt.Sprintf("%s/c/%s", rumbleBaseURL, info.ItemID)
	case model.TypeUser:
		pageURL = fmt.Sprintf("%s/user/%s", rumbleBaseURL, info.ItemID)
	default:
		return nil, errors.Errorf("unsupported rumble feed type: %s", info.LinkType)
	}

	result := &model.Feed{
		ItemID:    info.ItemID,
		Provider:  info.Provider,
		LinkType:  info.LinkType,
		Format:    cfg.Format,
		Quality:   cfg.Quality,
		PageSize:  cfg.PageSize,
		UpdatedAt: time.Now().UTC(),
		ItemURL:   pageURL,
	}

	var allEpisodes []*model.Episode
	remaining := cfg.PageSize

	for page := 1; remaining > 0; page++ {
		fetchURL := pageURL
		if page > 1 {
			fetchURL = fmt.Sprintf("%s?page=%d", pageURL, page)
		}

		log.Debugf("fetching rumble page %d: %s", page, fetchURL)
		doc, err := b.fetchPage(ctx, fetchURL)
		if err != nil {
			if page == 1 {
				return nil, errors.Wrap(err, "failed to fetch rumble page")
			}
			log.WithError(err).Warnf("failed to fetch rumble page %d, stopping pagination", page)
			break
		}

		if page == 1 {
			b.extractFeedMetadata(doc, result)
		}

		episodes, err := b.extractEpisodes(doc, remaining)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to extract episodes from page %d", page)
		}

		if len(episodes) == 0 {
			break
		}

		allEpisodes = append(allEpisodes, episodes...)
		remaining -= len(episodes)

		// Check if there are more pages
		if !b.hasNextPage(doc, page) {
			break
		}
	}

	result.Episodes = allEpisodes
	return result, nil
}

func (b *RumbleBuilder) fetchPage(ctx context.Context, pageURL string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Podsync)")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch page")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse HTML")
	}

	return doc, nil
}

func (b *RumbleBuilder) hasNextPage(doc *goquery.Document, currentPage int) bool {
	nextPage := fmt.Sprintf("page=%d", currentPage+1)
	return doc.Find(fmt.Sprintf(`a[href*="%s"]`, nextPage)).Length() > 0
}

func (b *RumbleBuilder) extractFeedMetadata(doc *goquery.Document, f *model.Feed) {
	// Try channel header first
	if title := strings.TrimSpace(doc.Find(".channel-header--title h1").Text()); title != "" {
		f.Title = title
		f.Author = title
	}

	if src, exists := doc.Find("img.channel-header--img").Attr("src"); exists {
		f.CoverArt = src
	}

	// Fallback to og: meta tags
	if f.Title == "" {
		f.Title = doc.Find(`meta[property="og:title"]`).AttrOr("content", f.ItemID)
		f.Author = f.Title
	}

	if f.CoverArt == "" {
		f.CoverArt = doc.Find(`meta[property="og:image"]`).AttrOr("content", "")
	}

	f.Description = doc.Find(`meta[property="og:description"]`).AttrOr("content", "")
}

func (b *RumbleBuilder) extractEpisodes(doc *goquery.Document, pageSize int) ([]*model.Episode, error) {
	var episodes []*model.Episode

	doc.Find("div.videostream.thumbnail__grid--item").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		// Skip live streams
		if s.Find(".videostream__status--live").Length() > 0 {
			return true
		}

		episode, err := b.parseVideoItem(s)
		if err != nil {
			log.WithError(err).Warn("failed to parse rumble video item, skipping")
			return true
		}

		episodes = append(episodes, episode)
		return len(episodes) < pageSize
	})

	return episodes, nil
}

func (b *RumbleBuilder) parseVideoItem(s *goquery.Selection) (*model.Episode, error) {
	href, exists := s.Find("a.videostream__link").Attr("href")
	if !exists {
		return nil, errors.New("no video link found")
	}

	// Strip query parameters from the href
	cleanPath := href
	if idx := strings.Index(cleanPath, "?"); idx != -1 {
		cleanPath = cleanPath[:idx]
	}

	// Extract the stable video token (e.g. "v1abc" from "/v1abc-some-title.html")
	// to use as the episode ID, since the slug portion can change if the title is edited.
	videoID := extractRumbleVideoID(cleanPath)

	videoURL := rumbleBaseURL + href
	title := strings.TrimSpace(s.Find("h3.thumbnail__title").Text())

	var pubDate time.Time
	if dt, exists := s.Find("time.videostream__time").Attr("datetime"); exists {
		if parsed, err := time.Parse(time.RFC3339, dt); err == nil {
			pubDate = parsed
		}
	}

	thumbnail, _ := s.Find("img.thumbnail__image").Attr("src")

	durationText := strings.TrimSpace(s.Find(".videostream__status--duration").Text())
	durationSeconds := parseDuration(durationText)

	return &model.Episode{
		ID:        videoID,
		Title:     title,
		Thumbnail: thumbnail,
		Duration:  durationSeconds,
		Size:      durationSeconds * 33013, // Rough estimate like Twitch builder
		VideoURL:  videoURL,
		PubDate:   pubDate,
		Status:    model.EpisodeNew,
	}, nil
}

// parseDuration parses a duration string in mm:ss or hh:mm:ss format into seconds.
func parseDuration(s string) int64 {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		m, err1 := strconv.ParseInt(parts[0], 10, 64)
		sec, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil {
			return 0
		}
		return m*60 + sec
	case 3:
		h, err1 := strconv.ParseInt(parts[0], 10, 64)
		m, err2 := strconv.ParseInt(parts[1], 10, 64)
		sec, err3 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0
		}
		return h*3600 + m*60 + sec
	default:
		return 0
	}
}

// extractRumbleVideoID extracts the stable video token from a Rumble video path.
// For example, "/v1abc-some-title.html" returns "v1abc".
// The slug after the first hyphen can change if the video title is edited,
// but the token prefix remains stable.
func extractRumbleVideoID(path string) string {
	// Remove leading slash
	name := strings.TrimPrefix(path, "/")
	// Remove .html suffix
	name = strings.TrimSuffix(name, ".html")
	// Extract token before first hyphen (e.g. "v1abc" from "v1abc-some-title")
	if idx := strings.Index(name, "-"); idx > 0 {
		return name[:idx]
	}
	return name
}
