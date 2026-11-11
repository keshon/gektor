package config

func validProfile() Profile {
	return Profile{
		Version: 1,
		Provider: Provider{
			Kind:  "llama",
			URL:   "http://127.0.0.1:8080",
			Model: "local",
		},
		Generation: Generation{
			MaxTokens:    256,
			ContextLimit: 4096,
		},
	}
}
