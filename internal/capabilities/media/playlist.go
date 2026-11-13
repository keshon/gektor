package media

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kkdai/youtube/v2"
)

type playlistEntry struct {
	Index           int     `json:"index"`
	URL             string  `json:"url"`
	Title           string  `json:"title"`
	Author          string  `json:"author"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type playlistPage struct {
	Backend    string          `json:"backend"`
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Author     string          `json:"author"`
	Entries    []playlistEntry `json:"entries"`
	NextOffset int             `json:"next_offset"`
	More       bool            `json:"more"`
	Total      *int            `json:"total,omitempty"`
}

func (s *Service) playlist(ctx context.Context, in VideoRequest) (playlistPage, error) {
	client := youtube.Client{HTTPClient: s.youtubeHTTP(ctx)}
	list, err := client.GetPlaylistContext(ctx, in.URL)
	if err != nil {
		return playlistPage{}, sourceFailure(err)
	}

	return pagePlaylist(list, in)
}

func pagePlaylist(list *youtube.Playlist, in VideoRequest) (playlistPage, error) {
	if in.Offset > len(list.Videos) {
		return playlistPage{}, fmt.Errorf("playlist offset exceeds %d entries", len(list.Videos))
	}

	page := playlistPage{
		Backend:    "native",
		ID:         list.ID,
		Title:      brief([]byte(list.Title), 512),
		Author:     brief([]byte(list.Author), 256),
		Entries:    []playlistEntry{},
		NextOffset: in.Offset,
		Total:      new(len(list.Videos)),
	}

	end := min(in.Offset+in.Limit, len(list.Videos))
	for i := in.Offset; i < end; i++ {
		video := list.Videos[i]
		entry := playlistEntry{
			Index:           i + 1,
			URL:             "https://www.youtube.com/watch?v=" + video.ID,
			Title:           brief([]byte(video.Title), 128),
			Author:          brief([]byte(video.Author), 64),
			DurationSeconds: video.Duration.Seconds(),
		}
		page.Entries = append(page.Entries, entry)
		page.NextOffset = i + 1
	}
	page.More = page.NextOffset < *page.Total

	return page, nil
}

func externalPlaylist(raw []byte, in VideoRequest) (playlistPage, error) {
	var source struct {
		ID, Title, Uploader string
		Total               *int `json:"playlist_count"`
		Entries             []*struct {
			ID, Title, Uploader string
			Duration            float64
			Index               int `json:"playlist_index"`
		}
	}
	err := json.Unmarshal(raw, &source)
	if err != nil {
		return playlistPage{}, err
	}

	page := playlistPage{
		Backend:    "yt-dlp",
		ID:         source.ID,
		Title:      brief([]byte(source.Title), 512),
		Author:     brief([]byte(source.Uploader), 256),
		Entries:    []playlistEntry{},
		NextOffset: in.Offset,
		Total:      source.Total,
	}
	end := in.Offset + in.Limit
	for i, video := range source.Entries {
		index := in.Offset + i + 1
		if video != nil && video.Index > 0 {
			index = video.Index
		}
		if index > end {
			page.More = true
			break
		}
		page.NextOffset = index
		if video == nil {
			continue
		}
		entry := playlistEntry{
			Index:           index,
			Title:           brief([]byte(video.Title), 128),
			Author:          brief([]byte(video.Uploader), 64),
			DurationSeconds: video.Duration,
		}
		if video.ID != "" {
			entry.URL = "https://www.youtube.com/watch?v=" + video.ID
		}
		page.Entries = append(page.Entries, entry)
	}
	if page.More {
		page.NextOffset = end
	}

	return page, nil
}
