package memes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	redditListingLimit  = 50
	redditUserAgent     = "elaxerbot-meme-sender/1.0 (+https://github.com/elaxer/elaxerbot)"
	memeAPITemplate     = "https://meme-api.com/gimme/%s"
	redditRetryInterval = 500 * time.Millisecond
)

var redditListingHosts = [...]string{
	"https://api.reddit.com",
	"https://www.reddit.com",
	"https://old.reddit.com",
}

var allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".webp"}

type memePost struct {
	Title     string
	ImageURL  string
	Permalink string
	Subreddit string
}

func memeIdentifier(m memePost) string {
	if id := strings.TrimSpace(m.Permalink); id != "" {
		return id
	}
	return strings.TrimSpace(m.ImageURL)
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
	return candidates[rng.Intn(len(candidates))], nil
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
	if post.PostHint == "image" {
		return direct
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

func hasAllowedImageExt(path string) bool {
	for _, ext := range allowedImageExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
