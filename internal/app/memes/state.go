package memes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type chatDailyState struct {
	Title string `json:"title"`
}

type dailyStateFile struct {
	Chats map[string]chatDailyState `json:"chats"`
}

type chatMemeState struct {
	Title       string   `json:"title"`
	NextDueUnix int64    `json:"next_due_unix"`
	RecentMemes []string `json:"recent_memes,omitempty"`
}

type memeStateFile struct {
	Chats map[string]chatMemeState `json:"chats"`
}

func loadKnownChats(path string) (map[int64]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int64]string{}, nil
		}
		return nil, fmt.Errorf("read known chats file: %w", err)
	}
	var payload dailyStateFile
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("parse known chats file: %w", err)
	}
	chats := make(map[int64]string, len(payload.Chats))
	for rawID, chat := range payload.Chats {
		chatID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			continue
		}
		chats[chatID] = strings.TrimSpace(chat.Title)
	}
	return chats, nil
}

func loadMemeState(path string) (map[int64]chatMemeState, error) {
	state := make(map[int64]chatMemeState)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, fmt.Errorf("read meme state: %w", err)
	}
	if len(b) == 0 {
		return state, nil
	}
	var payload memeStateFile
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("parse meme state: %w", err)
	}
	for rawID, chat := range payload.Chats {
		chatID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			continue
		}
		state[chatID] = chat
	}
	return state, nil
}

func saveMemeState(path string, state map[int64]chatMemeState) error {
	payload := memeStateFile{Chats: make(map[string]chatMemeState, len(state))}
	for chatID, chat := range state {
		payload.Chats[strconv.FormatInt(chatID, 10)] = chat
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meme state: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write meme state: %w", err)
	}
	return nil
}
