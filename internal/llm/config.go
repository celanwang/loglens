// Package llm 封装 OpenAI 兼容端点的 LLM 调用，通过 Provider 预设同时支持
// DeepSeek 与阿里云百炼 qwen（DeepSeek 优先）；配置统一从 .env 文件与
// 进程环境变量读取，调用失败时由调用方降级为规则摘要。
package llm

import (
	"os"
	"strings"
	"time"
)

// Provider 预设端点与默认模型；LLM_BASE_URL / LLM_MODEL 可显式覆盖。
type Provider struct {
	Name    string
	BaseURL string
	Model   string
	KeyEnv  string
}

var providers = map[string]Provider{
	"deepseek": {Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", KeyEnv: "DEEPSEEK_API_KEY"},
	"qwen":     {Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.7-plus", KeyEnv: "DASHSCOPE_API_KEY"},
}

// defaultProvider 未显式指定且无法推断时的兜底 Provider（DeepSeek 优先）。
const defaultProvider = "deepseek"

const defaultTimeout = 30 * time.Second

// Config LLM 接入配置。
type Config struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
	Enabled  bool
}

// LoadConfig 读取 dotenv 文件与进程环境变量生成配置；进程环境变量优先。
func LoadConfig(dotenv string) Config {
	vals := readDotEnv(dotenv)
	get := func(k string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return vals[k]
	}
	name := strings.ToLower(get("LLM_PROVIDER"))
	if name == "" || name == "auto" {
		name = detectProvider(get)
	}
	p, ok := providers[name]
	if !ok {
		p = providers[defaultProvider]
	}
	cfg := Config{
		Provider: p.Name,
		APIKey:   resolveKey(get, p),
		BaseURL:  p.BaseURL,
		Model:    p.Model,
		Timeout:  defaultTimeout,
		Enabled:  true,
	}
	if v := get("LLM_BASE_URL"); v != "" {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}
	if v := get("LLM_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := get("LLM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Timeout = d
		}
	}
	switch strings.ToLower(get("LLM_ENABLED")) {
	case "false", "0", "off", "no":
		cfg.Enabled = false
	}
	return cfg
}

// detectProvider 未显式指定 LLM_PROVIDER 时按已配置的 Key 推断：DeepSeek 优先。
func detectProvider(get func(string) string) string {
	if get("DEEPSEEK_API_KEY") != "" || get("LLM_API_KEY") != "" {
		return "deepseek"
	}
	if get("DASHSCOPE_API_KEY") != "" {
		return "qwen"
	}
	return defaultProvider
}

// resolveKey 解析 API Key：通用 LLM_API_KEY 优先，其次当前 Provider 的 Key，
// 最后回退另一家 Provider 的 Key（兼容两家共用一个 sk- 条目的场景）。
func resolveKey(get func(string) string, p Provider) string {
	if v := get("LLM_API_KEY"); v != "" {
		return v
	}
	if v := get(p.KeyEnv); v != "" {
		return v
	}
	for _, other := range providers {
		if other.Name != p.Name {
			if v := get(other.KeyEnv); v != "" {
				return v
			}
		}
	}
	return ""
}

// Available 报告配置是否足以发起真实调用。
func (c Config) Available() bool {
	return c.Enabled && c.APIKey != "" && c.APIKey != "sk-xxx"
}

func readDotEnv(path string) map[string]string {
	vals := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return vals
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return vals
}
