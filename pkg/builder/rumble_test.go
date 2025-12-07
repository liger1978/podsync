package builder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

type fakeRumbleDownloader struct {
	metadata ytdl.PlaylistMetadata
	entries  ytdl.PlaylistMetadata
	err      error
}

func (f fakeRumbleDownloader) PlaylistMetadata(_ context.Context, _ string) (ytdl.PlaylistMetadata, error) {
	return f.metadata, f.err
}

func (f fakeRumbleDownloader) PlaylistEntries(_ context.Context, _ string, _ int) (ytdl.PlaylistMetadata, error) {
	return f.entries, f.err
}

func TestRumbleBuilderBuild(t *testing.T) {
	ctx := context.Background()
	pubTime := time.Unix(1700000000, 0).UTC()
	builder, err := NewRumbleBuilder(fakeRumbleDownloader{
		entries: ytdl.PlaylistMetadata{
			Title:       "Channel title",
			Description: "Channel description",
			Channel:     "Channel author",
			ChannelUrl:  "https://rumble.com/c/example",
			Thumbnails: []ytdl.PlaylistMetadataThumbnail{
				{Url: "https://image/low.jpg"},
				{Url: "https://image/high.jpg"},
			},
			Entries: []*ytdl.PlaylistEntry{
				{
					ID:         "abc123",
					Title:      "First",
					Duration:   120,
					Timestamp:  pubTime.Unix(),
					WebpageURL: "https://rumble.com/vabc123",
					Thumbnails: []ytdl.PlaylistMetadataThumbnail{{Url: "https://image/episode1.jpg"}},
				},
				{
					ID:         "def456",
					Title:      "Second",
					Duration:   60,
					Timestamp:  pubTime.Add(1 * time.Hour).Unix(),
					WebpageURL: "https://rumble.com/vdef456",
					Thumbnails: []ytdl.PlaylistMetadataThumbnail{{Url: "https://image/episode2.jpg"}},
				},
			},
		},
	})
	require.NoError(t, err)

	cfg := &feed.Config{
		URL:          "https://rumble.com/c/example",
		PageSize:     1,
		Format:       model.FormatVideo,
		Quality:      model.QualityHigh,
		PlaylistSort: model.SortingDesc,
	}

	feed, err := builder.Build(ctx, cfg)
	require.NoError(t, err)
	require.Equal(t, model.ProviderRumble, feed.Provider)
	require.Equal(t, "Channel title", feed.Title)
	require.Equal(t, "Channel author", feed.Author)
	require.Len(t, feed.Episodes, 1)
	require.Equal(t, "abc123", feed.Episodes[0].ID)
	require.Equal(t, "https://rumble.com/vabc123", feed.Episodes[0].VideoURL)
}
