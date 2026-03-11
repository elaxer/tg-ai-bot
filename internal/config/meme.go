package config

import (
	"fmt"
	"strings"
	"time"
)

type MemeConfig struct {
	Subreddits  []string      `yaml:"bot_meme_subreddits"`
	IntervalMin time.Duration `yaml:"bot_meme_interval_min"`
	IntervalMax time.Duration `yaml:"bot_meme_interval_max"`
}

func (c *MemeConfig) applyDefaults() {
	setDefaultNum(&c.IntervalMin, 5*time.Hour)
	setDefaultNum(&c.IntervalMax, 6*time.Hour)

	subreddits := make([]string, 0, len(c.Subreddits))
	for _, sub := range c.Subreddits {
		v := strings.ToLower(strings.TrimSpace(sub))
		if v != "" {
			subreddits = append(subreddits, v)
		}
	}
	c.Subreddits = subreddits
}

func (c *MemeConfig) validate() error {
	if c.IntervalMin <= 0 {
		return fmt.Errorf("bot_meme_interval_min must be > 0")
	}
	if c.IntervalMax <= 0 {
		return fmt.Errorf("bot_meme_interval_max must be > 0")
	}
	if c.IntervalMax < c.IntervalMin {
		return fmt.Errorf("bot_meme_interval_max must be >= bot_meme_interval_min")
	}
	return nil
}
