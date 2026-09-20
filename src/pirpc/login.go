package pirpc

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProviderEnv maps a pi provider (auth.json key) to its API-key env var.
// Source: pi docs/providers.md "Environment Variables or Auth File".
type ProviderEnv struct {
	Provider string // pi provider id
	Label    string // human label
	Env      string // env var holding the API key
}

// ProviderEnvs covers single-env-var API-key providers.
var ProviderEnvs = []ProviderEnv{
	{"anthropic", "Anthropic", "ANTHROPIC_API_KEY"},
	{"openai", "OpenAI", "OPENAI_API_KEY"},
	{"google", "Google Gemini", "GEMINI_API_KEY"},
	{"deepseek", "DeepSeek", "DEEPSEEK_API_KEY"},
	{"mistral", "Mistral", "MISTRAL_API_KEY"},
	{"groq", "Groq", "GROQ_API_KEY"},
	{"cerebras", "Cerebras", "CEREBRAS_API_KEY"},
	{"xai", "xAI", "XAI_API_KEY"},
	{"openrouter", "OpenRouter", "OPENROUTER_API_KEY"},
	{"zai", "ZAI Coding Plan", "ZAI_API_KEY"},
	{"opencode", "OpenCode Zen", "OPENCODE_API_KEY"},
	{"kimi-coding", "Kimi For Coding", "KIMI_API_KEY"},
	{"minimax", "MiniMax", "MINIMAX_API_KEY"},
	{"together", "Together AI", "TOGETHER_API_KEY"},
	{"fireworks", "Fireworks", "FIREWORKS_API_KEY"},
	{"huggingface", "Hugging Face", "HF_TOKEN"},
	{"nvidia", "NVIDIA NIM", "NVIDIA_API_KEY"},
	{"vercel-ai-gateway", "Vercel AI Gateway", "AI_GATEWAY_API_KEY"},
	{"moonshot", "Moonshot", "MOONSHOT_API_KEY"},
	{"qwen-token-plan", "Qwen Token Plan", "QWEN_TOKEN_PLAN_API_KEY"},
}

// LookupEnv returns the env var for a provider id, or "".
func LookupEnv(provider string) string {
	for _, p := range ProviderEnvs {
		if p.Provider == provider {
			return p.Env
		}
	}
	return ""
}

// KeyPath is ~/.config/pitago/keys.json (0600): {ENV_VAR: key}.
func KeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pitago", "keys.json")
}

// RecentPath is ~/.config/pitago/recent_models.json: [{provider, id, label}].
func RecentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pitago", "recent_models.json")
}

// LoadKeys reads the keystore (missing file → empty map, no error).
func LoadKeys(path string) map[string]string {
	keys := make(map[string]string)
	if path == "" {
		return keys
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return keys
	}
	_ = json.Unmarshal(raw, &keys)
	return keys
}

// SaveKey stores one key (creates dir 0700, file 0600).
func SaveKey(path, env, key string) error {
	keys := LoadKeys(path)
	keys[env] = key
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// DeleteKey removes one key (missing → nil).
func DeleteKey(path, env string) error {
	keys := LoadKeys(path)
	if _, ok := keys[env]; !ok {
		return nil
	}
	delete(keys, env)
	raw, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
