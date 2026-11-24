package social

import "regexp"

var (
	handlePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
	numericPostID = regexp.MustCompile(`^[0-9]{1,25}$`)
)

// providerPost is the shared v1/v2 post shape; tombstones retain their message.
type providerPost struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`

	URL       string `json:"url"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Author    struct {
		Name       string `json:"name"`
		ScreenName string `json:"screen_name"`
	} `json:"author"`
	Media struct {
		Photos []struct {
			URL string `json:"url"`
		} `json:"photos"`
		Videos []struct {
			URL string `json:"url"`
		} `json:"videos"`
	} `json:"media"`
}

func (p providerPost) post(retrievedAt string) Post {
	out := Post{Status: "ok", Author: p.Author.Name, Handle: p.Author.ScreenName, Text: p.Text, CreatedAt: p.CreatedAt, Source: "FxTwitter", RetrievedAt: retrievedAt}
	if p.Type == "tombstone" {
		out.Status = "unavailable"
		out.Text = p.Message
		return out
	}
	id, err := postID(p.URL)
	if err != nil && numericPostID.MatchString(p.ID) {
		id = p.ID
		err = nil
	}
	if err != nil || out.Handle == "" {
		out.Status = "invalid_response"
	} else {
		out.URL = "https://x.com/i/status/" + id
	}
	for _, m := range p.Media.Photos {
		out.Media = append(out.Media, m.URL)
	}
	for _, m := range p.Media.Videos {
		out.Media = append(out.Media, m.URL)
	}
	return out
}
