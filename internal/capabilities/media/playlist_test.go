package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/process"
	"github.com/kkdai/youtube/v2"
)

func TestPlaylistPagesPreserveOrderAndUnavailableEntries(t *testing.T) {
	list := &youtube.Playlist{
		ID: "PL1234567890123",
		Videos: []*youtube.PlaylistEntry{
			{ID: "abcdefghijk", Title: "First", Duration: time.Minute},
			{ID: "lmnopqrstuv", Title: "Private video"},
			{ID: "12345678901", Title: "Third"},
		},
	}
	page, err := pagePlaylist(list, VideoRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Index != 1 || page.Entries[0].DurationSeconds != 60 || page.Entries[1].DurationSeconds != 0 || !page.More || page.NextOffset != 2 {
		t.Fatalf("first page: %+v", page)
	}

	page, err = pagePlaylist(list, VideoRequest{Offset: page.NextOffset, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Index != 3 || page.More || page.NextOffset != 3 {
		t.Fatalf("last page: %+v", page)
	}
}

func TestPlaylistValidationBeforeNetwork(t *testing.T) {
	s := testService(t)
	for _, in := range []VideoRequest{
		{URL: "https://vimeo.com/123?list=PL1234567890123"},
		{URL: "https://youtube.com/watch?v=abcdefghijk"},
		{URL: "https://youtube.com/playlist?list=PL1234567890123", Limit: 101},
	} {
		_, err := s.ValidateVideo("video_playlist", in)
		if err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}

	in, err := s.ValidateVideo("video_playlist", VideoRequest{URL: "https://youtube.com/playlist?list=PL1234567890123"})
	if err != nil {
		t.Fatal(err)
	}
	if backend(in) != "native" || in.Limit != 50 {
		t.Fatalf("defaults: %+v", in)
	}
}

func TestMergedAudioErrorExplainsSourceDownload(t *testing.T) {
	s := testService(t)
	_, err := s.ValidateVideo("video_download", VideoRequest{URL: "https://youtu.be/abcdefghijk", Mode: "merged", Path: "track.mp3"})
	if err == nil || !strings.Contains(err.Error(), "mode=audio") || !strings.Contains(err.Error(), "media_convert") {
		t.Fatalf("unhelpful audio error: %v", err)
	}
}

func TestNativePlaylistRoundTrip(t *testing.T) {
	s := testService(t)
	s.transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "" || req.URL.Path == "/" {
			return response(200, "\nytcfg.set({\"INNERTUBE_CONTEXT\":{\"client\":{\"visitorData\":\"fixture\"}}})"), nil
		}
		if !strings.Contains(req.URL.Path, "/browse") {
			t.Fatalf("unexpected playlist request: %s", req.URL)
		}
		return response(200, `{"header":{"pageHeaderRenderer":{"pageTitle":"Fixture playlist"}},"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"playlistVideoListRenderer":{"contents":[{"playlistVideoRenderer":{"videoId":"abcdefghijk","title":{"runs":[{"text":"First track"}]},"shortBylineText":{"runs":[{"text":"Uploader"}]},"lengthSeconds":"60"}},{"playlistVideoRenderer":{"videoId":"lmnopqrstuv","title":{"runs":[{"text":"Second track"}]},"lengthSeconds":"120"}}]}}]}}}}]}}}`), nil
	})

	value, err := s.RunVideo(t.Context(), "video_playlist", VideoRequest{URL: "https://www.youtube.com/playlist?list=PL1234567890123", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(playlistPage)
	if page.Title != "Fixture playlist" || (page.Total == nil || *page.Total != 2) || len(page.Entries) != 1 || page.Entries[0].URL != "https://www.youtube.com/watch?v=abcdefghijk" || !page.More {
		t.Fatalf("unexpected playlist: %+v", page)
	}
}

func TestLivePlaylist(t *testing.T) {
	url := os.Getenv("GEKTOR_MEDIA_LIVE_PLAYLIST")
	if url == "" {
		t.Skip("set GEKTOR_MEDIA_LIVE_PLAYLIST to a public YouTube playlist URL")
	}

	s := testService(t)
	value, err := s.RunVideo(t.Context(), "video_playlist", VideoRequest{URL: url, Limit: 2, TimeoutSeconds: 40})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(playlistPage)
	if len(page.Entries) == 0 || len(page.Entries) > 2 || (page.Total != nil && *page.Total < len(page.Entries)) {
		t.Fatalf("unexpected playlist: %+v", page)
	}
	t.Logf("native playlist: %q, %d entries, returned %d, more=%t", page.Title, *page.Total, len(page.Entries), page.More)
}

func TestExternalPlaylistPages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		indices []int
		next    int
		more    bool
	}{
		{"nonzero", `{"entries":[{"id":"a","playlist_index":51},{"id":"b","playlist_index":52},{"id":"c","playlist_index":53}]}`, []int{51, 52}, 52, true},
		{"final", `{"entries":[{"id":"a","playlist_index":51}]}`, []int{51}, 51, false},
		{"empty", `{"entries":[]}`, nil, 50, false},
		{"unavailable", `{"entries":[{"title":"Private video","playlist_index":51},{"id":"b","playlist_index":53}]}`, []int{51}, 52, true},
		{"gap", `{"entries":[{"id":"b","playlist_index":52},{"id":"c","playlist_index":53}]}`, []int{52}, 52, true},
		{"null", `{"entries":[null,{"id":"b","playlist_index":52},{"id":"c","playlist_index":53}]}`, []int{52}, 52, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := externalPlaylist([]byte(tc.raw), VideoRequest{Offset: 50, Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			if page.NextOffset != tc.next || page.More != tc.more || len(page.Entries) != len(tc.indices) {
				t.Fatalf("unexpected page: %+v", page)
			}
			for i, index := range tc.indices {
				if page.Entries[i].Index != index {
					t.Fatalf("indices: %+v", page.Entries)
				}
			}
			raw, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"total"`) {
				t.Fatalf("invented total: %s", raw)
			}
		})
	}
}

func TestExternalPlaylistFetchesOnlyRequestedRange(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.run = func(ctx context.Context, req process.Request) (process.Result, error) {
		args := strings.Join(req.Args, " ")
		if !strings.Contains(args, "--flat-playlist --lazy-playlist --playlist-items 5001:5051") {
			t.Fatalf("unbounded playlist request: %s", args)
		}
		var raw strings.Builder
		raw.WriteString(`{"playlist_count":100000,"entries":[`)
		for index := 5001; index <= 5051; index++ {
			if index > 5001 {
				raw.WriteByte(',')
			}
			fmt.Fprintf(&raw, `{"id":"abcdefghijk","playlist_index":%d,"title":%q}`, index, strings.Repeat("x", 1024))
		}
		raw.WriteString("]}")
		if raw.Len() >= 1<<20 {
			t.Fatal("fixture exceeds safety cap")
		}
		return process.Result{Stdout: []byte(raw.String())}, nil
	}
	value, err := s.RunVideo(t.Context(), "video_playlist", VideoRequest{
		URL: "https://youtube.com/playlist?list=PL1234567890123", Backend: "yt-dlp", Offset: 5000, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(playlistPage)
	if len(page.Entries) != 50 || page.Entries[0].Index != 5001 || page.NextOffset != 5050 || !page.More || page.Total == nil || *page.Total != 100000 {
		t.Fatalf("unexpected page: %+v", page)
	}
}
