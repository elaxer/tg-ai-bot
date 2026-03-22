package chatbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (p *Processor) touchChat(chatID int64, title string) {
	title = strings.TrimSpace(title)
	nowUnix := time.Now().Unix()

	p.stateMu.Lock()
	state, exists := p.chats[chatID]
	changed := false
	if !exists {
		state = chatDailyState{Title: title, LastSentUnix: nowUnix}
		changed = true
	} else if title != "" && title != state.Title {
		state.Title = title
		changed = true
	}
	p.chats[chatID] = state
	p.stateMu.Unlock()

	if changed {
		if err := p.saveDailyState(); err != nil {
			logError("save daily state failed", "chat_id", chatID, "err", err)
		}
	}
}

func (p *Processor) loadDailyState() {
	b, err := os.ReadFile(dailyStatePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logError("read daily state failed", "path", dailyStatePath, "err", err)
		}

		return
	}

	var parsed dailyStateFile
	if err := json.Unmarshal(b, &parsed); err != nil {
		logError("parse daily state failed", "err", err)

		return
	}

	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	for rawID, state := range parsed.Chats {
		if chatID, err := strconv.ParseInt(rawID, 10, 64); err == nil {
			p.chats[chatID] = state
		}
	}
}

func (p *Processor) saveDailyState() error {
	p.stateMu.Lock()
	snapshot := make(map[string]chatDailyState, len(p.chats))
	for chatID, state := range p.chats {
		snapshot[strconv.FormatInt(chatID, 10)] = state
	}
	p.stateMu.Unlock()

	payload := dailyStateFile{Chats: snapshot}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daily state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dailyStatePath), 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	return os.WriteFile(dailyStatePath, b, 0o600)
}
