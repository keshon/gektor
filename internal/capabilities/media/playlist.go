package media

import (
	"context"
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
	Total      int             `json:"total"`
}

func (s *Service) playlist(ctx context.Context, in VideoRequest) (playlistPage, error) {
	client := youtube.Client{HTTPClient: s.youtubeHTTP(ctx)}
	list, err := client.GetPlaylistContext(ctx, in.URL)
	if err != nil {
		return playlistPage{}, fmt.Errorf("reading YouTube playlist: %w", err)
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
		Total:      len(list.Videos),
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
	page.More = page.NextOffset < page.Total

	return page, nil
}
