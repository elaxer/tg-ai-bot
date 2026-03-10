package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"telegram-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	defaultConfigPath     = "bot.config.yaml"
	defaultKnownChatsPath = "data/daily_messages_state.json"
	defaultMemeStatePath  = "data/meme_messages_state.json"
	defaultCheckInterval  = 15 * time.Minute
	defaultRequestTimeout = 20 * time.Second
	defaultSendPause      = 2 * time.Second
	redditListingLimit    = 50
	redditUserAgent       = "elaxerbot-meme-sender/1.0 (+https://github.com/elaxer/elaxerbot)"
	memeAPITemplate       = "https://meme-api.com/gimme/%s"
	memeFetchMaxAttempts  = 5
)

var redditListingHosts = []string{
	"https://api.reddit.com",
	"https://www.reddit.com",
	"https://old.reddit.com",
}

var allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".webp"}

type chatDailyState struct {
	Title string `json:"title"`
}

type dailyStateFile struct {
	Chats map[string]chatDailyState `json:"chats"`
}

type chatMemeState struct {
	Title       string `json:"title"`
	NextDueUnix int64  `json:"next_due_unix"`
}

type memeStateFile struct {
	Chats map[string]chatMemeState `json:"chats"`
}

type memeWorker struct {
	bot            *tgbotapi.BotAPI
	httpClient     *http.Client
	rng            *rand.Rand
	subreddits     []string
	minInterval    time.Duration
	maxInterval    time.Duration
	statePath      string
	knownChatsPath string
	requestTimeout time.Duration
	dryRun         bool
	state          map[int64]chatMemeState
}

type memePost struct {
	Title     string
	ImageURL  string
	Permalink string
	Subreddit string
}

func main() {
	var (
		configPath     string
		knownChatsPath string
		statePath      string
		checkInterval  time.Duration
		requestTimeout time.Duration
		dryRun         bool
		once           bool
	)

	flag.StringVar(&configPath, "config", defaultConfigPath, "path to bot config yaml")
	flag.StringVar(&knownChatsPath, "known-chats", defaultKnownChatsPath, "path to JSON file with known chats")
	flag.StringVar(&statePath, "state", defaultMemeStatePath, "path to meme sender state json")
	flag.DurationVar(&checkInterval, "check-interval", defaultCheckInterval, "how often to scan for due chats")
	flag.DurationVar(&requestTimeout, "request-timeout", defaultRequestTimeout, "timeout per subreddit request")
	flag.BoolVar(&dryRun, "dry-run", false, "log outgoing memes without sending them")
	flag.BoolVar(&once, "once", false, "run a single scan/send cycle and exit")
	flag.Parse()

	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	botCfg, err := config.LoadBot(configPath)
	if err != nil {
		log.Fatalf("load bot config: %v", err)
	}
	runtimeCfg, err := config.LoadRuntimeFromEnv()
	if err != nil {
		log.Fatalf("load runtime env: %v", err)
	}
	if len(botCfg.MemeSubreddits) == 0 {
		log.Fatalf("bot config must define at least one bot_meme_subreddits entry")
	}
	if checkInterval <= 0 {
		log.Fatalf("check-interval must be > 0")
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		log.Fatalf("create telegram client: %v", err)
	}

	client := &http.Client{Timeout: requestTimeout}
	worker := &memeWorker{
		bot:            tgBot,
		httpClient:     client,
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
		subreddits:     append([]string(nil), botCfg.MemeSubreddits...),
		minInterval:    botCfg.MemeIntervalMin,
		maxInterval:    botCfg.MemeIntervalMax,
		statePath:      statePath,
		knownChatsPath: knownChatsPath,
		requestTimeout: requestTimeout,
		dryRun:         dryRun,
	}
	if err := worker.loadState(); err != nil {
		log.Fatalf("load meme state: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		scanned, sent, err := worker.runCycle(ctx)
		if err != nil {
			log.Fatalf("run cycle failed: %v", err)
		}
		log.Printf("cycle done: due=%d sent=%d", scanned, sent)
		return
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	log.Printf("meme worker started: known=%s state=%s min=%s max=%s check=%s dry-run=%t", knownChatsPath, statePath, worker.minInterval, worker.maxInterval, checkInterval, dryRun)
	if _, _, err := worker.runCycle(ctx); err != nil {
		log.Printf("initial cycle failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("meme worker stopped")
			return
		case <-ticker.C:
			if _, _, err := worker.runCycle(ctx); err != nil {
				log.Printf("cycle failed: %v", err)
			}
		}
	}
}

func (w *memeWorker) loadState() error {
	state, err := loadMemeState(w.statePath)
	if err != nil {
		return err
	}
	w.state = state
	return nil
}

func (w *memeWorker) runCycle(ctx context.Context) (int, int, error) {
	knownChats, err := loadKnownChats(w.knownChatsPath)
	if err != nil {
		return 0, 0, err
	}
	if len(knownChats) == 0 {
		log.Printf("no known chats found in %s", w.knownChatsPath)
		return 0, 0, nil
	}
	changed := w.ensureChats(knownChats)
	if changed {
		if err := saveMemeState(w.statePath, w.state); err != nil {
			log.Printf("save meme state failed: %v", err)
		}
	}

	dueChats := w.collectDueChats(knownChats)
	if len(dueChats) == 0 {
		log.Printf("no chats due (%d tracked)", len(knownChats))
		return 0, 0, nil
	}

	sent := 0
	for _, chatID := range dueChats {
		if ctx.Err() != nil {
			return len(dueChats), sent, ctx.Err()
		}
		if err := w.sendMeme(ctx, chatID, knownChats[chatID]); err != nil {
			log.Printf("send meme failed chat_id=%d title=%q err=%v", chatID, knownChats[chatID], err)
			continue
		}
		if !w.dryRun {
			w.scheduleNext(chatID, knownChats[chatID])
			if err := saveMemeState(w.statePath, w.state); err != nil {
				log.Printf("save meme state failed: %v", err)
			}
		}
		sent++
		time.Sleep(defaultSendPause)
	}
	return len(dueChats), sent, nil
}

func (w *memeWorker) ensureChats(known map[int64]string) bool {
	if w.state == nil {
		w.state = make(map[int64]chatMemeState)
	}
	changed := false
	for chatID, title := range known {
		state, exists := w.state[chatID]
		cleanTitle := strings.TrimSpace(title)
		if !exists {
			state = chatMemeState{}
			changed = true
		}
		if cleanTitle != "" && state.Title != cleanTitle {
			state.Title = cleanTitle
			changed = true
		}
		w.state[chatID] = state
	}
	return changed
}

func (w *memeWorker) collectDueChats(known map[int64]string) []int64 {
	nowUnix := time.Now().Unix()
	due := make([]int64, 0, len(known))
	for chatID := range known {
		state := w.state[chatID]
		if state.NextDueUnix == 0 || nowUnix >= state.NextDueUnix {
			due = append(due, chatID)
		}
	}
	return due
}

func (w *memeWorker) scheduleNext(chatID int64, title string) {
	state := w.state[chatID]
	if state.Title == "" {
		state.Title = strings.TrimSpace(title)
	}
	state.NextDueUnix = time.Now().Add(randomInterval(w.rng, w.minInterval, w.maxInterval)).Unix()
	w.state[chatID] = state
}

func (w *memeWorker) sendMeme(parentCtx context.Context, chatID int64, title string) error {
	if len(w.subreddits) == 0 {
		return errors.New("no subreddits configured")
	}
	var (
		meme      memePost
		lastErr   error
		subreddit string
	)
	for attempt := 1; attempt <= memeFetchMaxAttempts; attempt++ {
		select {
		case <-parentCtx.Done():
			return parentCtx.Err()
		default:
		}
		subreddit = w.subreddits[w.rng.Intn(len(w.subreddits))]
		meme, lastErr = fetchRandomMeme(parentCtx, w.httpClient, subreddit, w.requestTimeout, w.rng)
		if lastErr == nil {
			break
		}
		log.Printf("fetch meme attempt %d/%d failed chat_id=%d title=%q subreddit=%s err=%v", attempt, memeFetchMaxAttempts, chatID, title, subreddit, lastErr)
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("fetch meme subreddit=%s: %w", subreddit, lastErr)
	}
	if w.dryRun {
		log.Printf("dry-run meme chat_id=%d title=%q subreddit=%s image=%s", chatID, title, subreddit, meme.ImageURL)
		return nil
	}

	// captionParts := make([]string, 0, 3)
	// if strings.TrimSpace(meme.Title) != "" {
	// 	captionParts = append(captionParts, meme.Title)
	// }
	// captionParts = append(captionParts, fmt.Sprintf("r/%s", meme.Subreddit))
	// if meme.Permalink != "" {
	// 	captionParts = append(captionParts, meme.Permalink)
	// }

	msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(meme.ImageURL))
	//msg.Caption = strings.Join(captionParts, "\n")

	if _, err := w.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram photo: %w", err)
	}
	log.Printf("sent meme chat_id=%d title=%q subreddit=%s", chatID, title, subreddit)
	return nil
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
		chats[chatID] = chat.Title
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

func randomInterval(rng *rand.Rand, min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	span := max - min
	if span <= 0 {
		return min
	}
	return min + time.Duration(rng.Int63n(int64(span)))
}

func fetchRandomMeme(parent context.Context, client *http.Client, subreddit string, timeout time.Duration, rng *rand.Rand) (memePost, error) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()

	if pick, err := fetchFromRedditHosts(ctx, client, subreddit, rng); err == nil {
		return pick, nil
	}
	return fetchFromMemeAPI(ctx, client, subreddit)
}

type redditListing struct {
	Data struct {
		Children []struct {
			Data redditPost `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditPost struct {
	Title         string                 `json:"title"`
	URL           string                 `json:"url"`
	Permalink     string                 `json:"permalink"`
	Over18        bool                   `json:"over_18"`
	PostHint      string                 `json:"post_hint"`
	IsGallery     bool                   `json:"is_gallery"`
	MediaMetadata map[string]redditMedia `json:"media_metadata"`
	GalleryData   redditGalleryData      `json:"gallery_data"`
}

type redditMedia struct {
	S redditMediaSource `json:"s"`
}

type redditMediaSource struct {
	URL string `json:"u"`
}

type redditGalleryData struct {
	Items []redditGalleryItem `json:"items"`
}

type redditGalleryItem struct {
	MediaID string `json:"media_id"`
}

func resolveImageURL(post redditPost) string {
	direct := strings.TrimSpace(post.URL)
	if isDirectImageURL(direct) {
		return direct
	}
	if post.IsGallery {
		for _, item := range post.GalleryData.Items {
			media, ok := post.MediaMetadata[item.MediaID]
			if !ok {
				continue
			}
			if url := strings.TrimSpace(html.UnescapeString(media.S.URL)); url != "" && isDirectImageURL(url) {
				return url
			}
		}
	}
	return ""
}

func isDirectImageURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	path := strings.ToLower(u.Path)
	return hasAllowedImageExt(path)
}

func fetchFromRedditHosts(ctx context.Context, client *http.Client, subreddit string, rng *rand.Rand) (memePost, error) {
	var listing redditListing
	var lastErr error
	for _, host := range redditListingHosts {
		listing, lastErr = fetchRedditListing(ctx, client, host, subreddit)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return memePost{}, lastErr
	}

	candidates := make([]memePost, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		post := child.Data
		if post.Over18 {
			continue
		}
		imageURL := resolveImageURL(post)
		if imageURL == "" {
			continue
		}
		title := strings.TrimSpace(post.Title)
		candidates = append(candidates, memePost{
			Title:     title,
			ImageURL:  imageURL,
			Permalink: post.Permalink,
			Subreddit: subreddit,
		})
	}
	if len(candidates) == 0 {
		return memePost{}, errors.New("no suitable meme candidates")
	}
	pick := candidates[rng.Intn(len(candidates))]
	if pick.ImageURL == "" {
		return memePost{}, errors.New("missing image url")
	}
	return pick, nil
}

func fetchRedditListing(ctx context.Context, client *http.Client, baseURL, subreddit string) (redditListing, error) {
	var listing redditListing
	apiURL := fmt.Sprintf("%s/r/%s/hot.json?limit=%d&raw_json=1", strings.TrimRight(baseURL, "/"), url.PathEscape(subreddit), redditListingLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return listing, err
	}
	req.Header.Set("User-Agent", redditUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return listing, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return listing, fmt.Errorf("reddit status %s from %s", resp.Status, baseURL)
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return listing, fmt.Errorf("decode reddit listing: %w", err)
	}
	return listing, nil
}

func fetchFromMemeAPI(ctx context.Context, client *http.Client, subreddit string) (memePost, error) {
	apiURL := fmt.Sprintf(memeAPITemplate, url.PathEscape(subreddit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return memePost{}, err
	}
	req.Header.Set("User-Agent", redditUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return memePost{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return memePost{}, fmt.Errorf("meme-api status %s", resp.Status)
	}

	var payload memeAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return memePost{}, fmt.Errorf("decode meme-api: %w", err)
	}
	if payload.NSFW {
		return memePost{}, errors.New("meme-api returned nsfw meme")
	}
	imageURL := strings.TrimSpace(payload.URL)
	if !isDirectImageURL(imageURL) {
		return memePost{}, errors.New("meme-api url is not an image")
	}
	return memePost{
		Title:     strings.TrimSpace(payload.Title),
		ImageURL:  imageURL,
		Permalink: payload.PostLink,
		Subreddit: payload.Subreddit,
	}, nil
}

type memeAPIResponse struct {
	PostLink  string `json:"postLink"`
	Subreddit string `json:"subreddit"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	NSFW      bool   `json:"nsfw"`
}

func hasAllowedImageExt(path string) bool {
	for _, ext := range allowedImageExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
