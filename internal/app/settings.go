package app

import (
	"os"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/filetxn"
)

// SettingsFiles names the stores selected by a frontend. Empty paths are skipped.
type SettingsFiles struct {
	Custom, Session, Application, Archive string
	Preferences                           config.Preferences
}

func (s SettingsFiles) Save(profile config.Profile, conversation *chat.Conversation) error {
	var writes []filetxn.Write
	if s.Custom != "" {
		writes = append(writes, filetxn.Write{Path: s.Custom, Save: func(path string) error { return config.SaveCopy(profile, path) }})
	}
	save := func(path string) error { return config.SavePreferences(path, s.Preferences) }
	for _, path := range []string{s.Session, s.Application} {
		if path != "" {
			writes = append(writes, filetxn.Write{Path: path, Save: save})
		}
	}
	if conversation != nil && s.Archive != "" {
		if _, err := os.Stat(s.Archive); err == nil {
			writes = append(writes, filetxn.Write{Path: s.Archive, Save: conversation.Save})
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return filetxn.Replace(writes)
}
