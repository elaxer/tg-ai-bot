package config

type OpenAIConfig struct {
	Model           string `yaml:"openai_model"`
	TTSModel        string `yaml:"openai_tts_model"`
	SystemPrompt    string `yaml:"openai_system_prompt"`
	TTSVoice        string `yaml:"openai_tts_voice"`
	TTSInstructions string `yaml:"openai_tts_instructions"`
}

func (c *OpenAIConfig) applyDefaults() {
	setDefaultStr(&c.Model, "gpt-4.1-mini")
	setDefaultStr(&c.TTSModel, "gpt-4o-mini-tts")
	setDefaultStr(&c.SystemPrompt, defaultSystemPrompt)
	setDefaultStr(&c.TTSVoice, "alloy")
}
