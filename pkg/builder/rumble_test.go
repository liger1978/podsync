package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const testRumbleHTML = `<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="TestChannel" />
<meta property="og:description" content="A test channel description" />
<meta property="og:image" content="https://example.com/og-image.png" />
</head>
<body>
<div class="channel-header--container">
  <div class="channel-header--thumb">
    <img class="channel-header--img" src="https://example.com/avatar.png" alt="TestChannel">
  </div>
  <div class="channel-header--title">
    <div><h1>TestChannel</h1></div>
  </div>
</div>
<section class="channel-listing__container">
  <ol class="thumbnail__grid">
    <div class="videostream thumbnail__grid--item">
      <div class="thumbnail__thumb">
        <img class="thumbnail__image" src="https://example.com/thumb1.jpg" alt="Video One">
        <div class="videostream__info videostream__info--bottom">
          <div class="videostream__badge videostream__status videostream__status--duration">12:34</div>
        </div>
        <a class="videostream__link link" href="/v1abc-video-one.html?e9s=src_v1"></a>
      </div>
      <div class="videostream__footer">
        <a class="title__link link" href="/v1abc-video-one.html?e9s=src_v1">
          <h3 class="thumbnail__title">Video One</h3>
        </a>
        <address class="videostream__subfooter">
          <div class="videostream__data">
            <span class="videostream__data--item videostream__date">
              <time class="videostream__data--subitem videostream__time" datetime="2024-01-15T10:00:00-05:00">1 year ago</time>
            </span>
          </div>
        </address>
      </div>
    </div>
    <div class="videostream thumbnail__grid--item">
      <div class="thumbnail__thumb">
        <img class="thumbnail__image" src="https://example.com/thumb2.jpg" alt="Video Two">
        <div class="videostream__info videostream__info--bottom">
          <div class="videostream__badge videostream__status videostream__status--duration">1:05:30</div>
        </div>
        <a class="videostream__link link" href="/v2def-video-two.html?e9s=src_v1"></a>
      </div>
      <div class="videostream__footer">
        <a class="title__link link" href="/v2def-video-two.html?e9s=src_v1">
          <h3 class="thumbnail__title">Video Two</h3>
        </a>
        <address class="videostream__subfooter">
          <div class="videostream__data">
            <span class="videostream__data--item videostream__date">
              <time class="videostream__data--subitem videostream__time" datetime="2024-02-20T14:30:00-05:00">11 months ago</time>
            </span>
          </div>
        </address>
      </div>
    </div>
    <div class="videostream thumbnail__grid--item">
      <div class="thumbnail__thumb thumbnail__thumb--live">
        <img class="thumbnail__image" src="https://example.com/thumb-live.jpg" alt="Live Stream">
        <div class="videostream__info videostream__info--bottom">
          <div class="videostream__badge videostream__status videostream__status--live">LIVE</div>
        </div>
        <a class="videostream__link link" href="/v3ghi-live-stream.html"></a>
      </div>
      <div class="videostream__footer videostream__footer--live">
        <a class="title__link link" href="/v3ghi-live-stream.html">
          <h3 class="thumbnail__title">Live Stream Now</h3>
        </a>
      </div>
    </div>
  </ol>
</section>
</body>
</html>`

func TestRumbleBuilder_ExtractFeedMetadata(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(testRumbleHTML))
	require.NoError(t, err)

	b := &RumbleBuilder{}
	f := &model.Feed{ItemID: "TestChannel"}
	b.extractFeedMetadata(doc, f)

	assert.Equal(t, "TestChannel", f.Title)
	assert.Equal(t, "TestChannel", f.Author)
	assert.Equal(t, "https://example.com/avatar.png", f.CoverArt)
	assert.Equal(t, "A test channel description", f.Description)
}

func TestRumbleBuilder_ExtractFeedMetadata_Fallback(t *testing.T) {
	html := `<html><head>
<meta property="og:title" content="FallbackTitle" />
<meta property="og:description" content="Fallback desc" />
<meta property="og:image" content="https://example.com/fallback.png" />
</head><body></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	b := &RumbleBuilder{}
	f := &model.Feed{ItemID: "test"}
	b.extractFeedMetadata(doc, f)

	assert.Equal(t, "FallbackTitle", f.Title)
	assert.Equal(t, "https://example.com/fallback.png", f.CoverArt)
	assert.Equal(t, "Fallback desc", f.Description)
}

func TestRumbleBuilder_ExtractEpisodes(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(testRumbleHTML))
	require.NoError(t, err)

	b := &RumbleBuilder{}
	episodes, err := b.extractEpisodes(doc, 50)
	require.NoError(t, err)

	// Should have 2 episodes (live stream skipped)
	require.Len(t, episodes, 2)

	ep1 := episodes[0]
	assert.Equal(t, "/v1abc-video-one.html", ep1.ID)
	assert.Equal(t, "Video One", ep1.Title)
	assert.Equal(t, "https://example.com/thumb1.jpg", ep1.Thumbnail)
	assert.Equal(t, int64(754), ep1.Duration) // 12*60 + 34
	assert.Equal(t, "https://rumble.com/v1abc-video-one.html?e9s=src_v1", ep1.VideoURL)
	assert.Equal(t, model.EpisodeNew, ep1.Status)
	assert.False(t, ep1.PubDate.IsZero())

	ep2 := episodes[1]
	assert.Equal(t, "/v2def-video-two.html", ep2.ID)
	assert.Equal(t, "Video Two", ep2.Title)
	assert.Equal(t, int64(3930), ep2.Duration) // 1*3600 + 5*60 + 30
}

func TestRumbleBuilder_ExtractEpisodes_PageSize(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(testRumbleHTML))
	require.NoError(t, err)

	b := &RumbleBuilder{}
	episodes, err := b.extractEpisodes(doc, 1)
	require.NoError(t, err)

	// Should only get 1 episode due to page size
	require.Len(t, episodes, 1)
	assert.Equal(t, "Video One", episodes[0].Title)
}

func TestRumbleBuilder_SkipsLiveStreams(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(testRumbleHTML))
	require.NoError(t, err)

	b := &RumbleBuilder{}
	episodes, err := b.extractEpisodes(doc, 50)
	require.NoError(t, err)

	for _, ep := range episodes {
		assert.NotEqual(t, "Live Stream Now", ep.Title)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"5:30", 330},
		{"12:34", 754},
		{"0:00", 0},
		{"1:23:45", 5025},
		{"0:05:30", 330},
		{"", 0},
		{"invalid", 0},
		{"a:b", 0},
		{"1:2:3:4", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseDuration(tt.input))
		})
	}
}

func TestRumbleBuilder_Build_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	b, err := NewRumbleBuilder()
	require.NoError(t, err)

	cfg := &feed.Config{
		URL:      "https://rumble.com/c/Lotuseaters",
		PageSize: 5,
		Format:   model.FormatVideo,
		Quality:  model.QualityHigh,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotEmpty(t, result.Title)
	require.NotEmpty(t, result.Episodes)

	for _, ep := range result.Episodes {
		assert.NotEmpty(t, ep.ID)
		assert.NotEmpty(t, ep.Title)
		assert.NotEmpty(t, ep.VideoURL)
	}
}
