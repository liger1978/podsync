package builder

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/pkg/errors"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

const (
	rumbleHighVideoBytesPerSecond = 350000
	rumbleLowVideoBytesPerSecond  = 100000
	rumbleHighAudioBytesPerSecond = 128000 / 8
	rumbleLowAudioBytesPerSecond  = 48000 / 8
)

type RumbleBuilder struct {
	downloader Downloader
}

func NewRumbleBuilder(downloader Downloader) (*RumbleBuilder, error) {
	if downloader == nil {
		return nil, errors.New("downloader is required")
	}

	return &RumbleBuilder{downloader: downloader}, nil
}

func (r *RumbleBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	metadata, err := r.downloader.PlaylistEntries(ctx, cfg.URL, cfg.PageSize)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load rumble metadata")
	}

	_feed := &model.Feed{
		ItemID:          info.ItemID,
		Provider:        info.Provider,
		LinkType:        info.LinkType,
		Format:          cfg.Format,
		Quality:         cfg.Quality,
		PageSize:        cfg.PageSize,
		PlaylistSort:    cfg.PlaylistSort,
		CoverArtQuality: cfg.Custom.CoverArtQuality,
		UpdatedAt:       time.Now().UTC(),
	}

	if metadata.Title != "" {
		_feed.Title = metadata.Title
	} else {
		_feed.Title = info.ItemID
	}

	_feed.Description = metadata.Description
	if _feed.Description == "" {
		_feed.Description = fmt.Sprintf("Rumble feed for %s", _feed.Title)
	}

	_feed.Author = metadata.Channel
	if _feed.Author == "" {
		_feed.Author = _feed.Title
	}

	_feed.ItemURL = metadata.ChannelUrl
	if _feed.ItemURL == "" {
		_feed.ItemURL = metadata.WebpageUrl
	}
	if _feed.ItemURL == "" {
		_feed.ItemURL = cfg.URL
	}

	_feed.CoverArt = selectRumbleThumbnail(metadata.Thumbnails, _feed.CoverArtQuality)

	for _, entry := range metadata.Entries {
		if entry == nil || entry.ID == "" {
			continue
		}

		_feed.Episodes = append(_feed.Episodes, convertRumbleEntry(entry, _feed))

		if len(_feed.Episodes) >= _feed.PageSize {
			break
		}
	}

	sortEpisodes(_feed)

	if _feed.PubDate.IsZero() && len(_feed.Episodes) > 0 {
		_feed.PubDate = _feed.Episodes[0].PubDate
	}

	return _feed, nil
}

func sortEpisodes(feed *model.Feed) {
	if len(feed.Episodes) == 0 {
		return
	}

	sort.SliceStable(feed.Episodes, func(i, j int) bool {
		if feed.PlaylistSort == model.SortingDesc {
			return feed.Episodes[i].PubDate.After(feed.Episodes[j].PubDate)
		}
		return feed.Episodes[i].PubDate.Before(feed.Episodes[j].PubDate)
	})
}

func selectRumbleThumbnail(thumbnails []ytdl.PlaylistMetadataThumbnail, quality model.Quality) string {
	if len(thumbnails) == 0 {
		return ""
	}

	if quality == model.QualityLow {
		return thumbnails[0].Url
	}

	return thumbnails[len(thumbnails)-1].Url
}

func convertRumbleEntry(entry *ytdl.PlaylistEntry, feed *model.Feed) *model.Episode {
	pubDate := publishedAt(entry)
	if pubDate.IsZero() {
		pubDate = time.Now().UTC()
	}

	duration := int64(entry.Duration)
	image := selectRumbleThumbnail(entry.Thumbnails, feed.Quality)

	videoURL := entry.WebpageURL
	if videoURL == "" {
		videoURL = entry.URL
	}
	if videoURL == "" {
		videoURL = fmt.Sprintf("https://rumble.com/%s", entry.ID)
	}

	return &model.Episode{
		ID:          entry.ID,
		Title:       entry.Title,
		Description: entry.Description,
		Duration:    duration,
		PubDate:     pubDate,
		Thumbnail:   image,
		VideoURL:    videoURL,
		Size:        estimateRumbleSize(duration, feed),
		Status:      model.EpisodeNew,
	}
}

func publishedAt(entry *ytdl.PlaylistEntry) time.Time {
	if entry == nil {
		return time.Time{}
	}

	if entry.Timestamp > 0 {
		return time.Unix(entry.Timestamp, 0).UTC()
	}

	if entry.ReleaseTimestamp > 0 {
		return time.Unix(entry.ReleaseTimestamp, 0).UTC()
	}

	if entry.UploadDate != "" {
		if t, err := time.Parse("20060102", entry.UploadDate); err == nil {
			return t
		}
	}

	return time.Time{}
}

func estimateRumbleSize(duration int64, feed *model.Feed) int64 {
	if feed.Format == model.FormatAudio {
		if feed.Quality == model.QualityHigh {
			return int64(rumbleHighAudioBytesPerSecond) * duration
		}
		return int64(rumbleLowAudioBytesPerSecond) * duration
	}

	if feed.Quality == model.QualityHigh {
		return duration * rumbleHighVideoBytesPerSecond
	}
	return duration * rumbleLowVideoBytesPerSecond
}
