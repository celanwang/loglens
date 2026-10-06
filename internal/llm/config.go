// Package llm 封装阿里云百炼（DashScope）OpenAI 兼容端点的调用，
// 配置统一从 .env 文件与进程环境变量读取；调用失败时由调用方降级为规则摘要。
package llm

import (
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultModel   = "qwen3.7-plus"
	defaultTimeout = 30 * time.Second
)

// Config LLM 接入配置。
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	Enabled bool
}

// LoadConfig 读取 dotenv 文件与进程环境变量生成配置；进程环境变量优先。
func LoadConfig(dotenv string) Config {
	cfg := Config{BaseURL: defaultBaseURL, Model: defaultModel, Timeout: defaultTimeout, Enabled: true}
	vals := readDotEnv(dotenv)
	get := func(k string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return vals[k]
	}
	cfg.APIKey = get("DASHSCOPE_API_KEY")
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
