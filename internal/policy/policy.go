// Package policy 定义 goto-mcp 的 Agent 访问策略。
// 策略描述「Agent 允许连接什么、做什么」，与描述「如何连接」的 goto 配置分离。
package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Policy 是 goto-mcp 的 Agent 策略。
type Policy struct {
	Version            int      `yaml:"version"`
	AllowedJumpers     []string `yaml:"allowed_jumpers"`
	TargetPatterns     []string `yaml:"target_patterns"`
	AllowSessionInput  bool     `yaml:"allow_session_input"`
	HostKeyPolicy      string   `yaml:"host_key_policy"`
	IdleTimeoutSeconds int      `yaml:"idle_timeout_seconds"`
	MaxSessionSeconds  int      `yaml:"max_session_seconds"`
	MaxOutputBytes     int      `yaml:"max_output_bytes"`
	CredentialProvider string   `yaml:"credential_provider"`

	allowedJumpers map[string]bool
	targetRegexps  []*regexp.Regexp
}

// Defaults 返回 v1 固定默认策略：只读、仅信任 known_hosts、凭据走系统安全存储。
func Defaults() Policy {
	return Policy{
		Version:            1,
		AllowSessionInput:  false,
		HostKeyPolicy:      "known_hosts_only",
		IdleTimeoutSeconds: 900,
		MaxSessionSeconds:  1800,
		MaxOutputBytes:     65536,
		CredentialProvider: "os",
	}
}

// DefaultPath 返回默认策略文件路径：~/.goto/agent-policy.yaml。
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "agent-policy.yaml"
	}
	return filepath.Join(home, ".goto", "agent-policy.yaml")
}

// Load 从指定路径加载策略；文件不存在时返回错误提示生成示例。
// 路径前缀 ~/ 会展开为用户主目录（MCP 客户端直接 exec，不经 shell）。
func Load(path string) (Policy, error) {
	path = expandTilde(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Policy{}, fmt.Errorf("Agent 策略文件不存在: %s（可用 -init-agent-policy 生成示例）", path)
		}
		return Policy{}, err
	}
	p := Defaults()
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	// v1 固定能力边界：策略文件无法放开这三项。
	p.AllowSessionInput = false
	p.HostKeyPolicy = "known_hosts_only"
	p.CredentialProvider = "os"
	p.allowedJumpers = make(map[string]bool, len(p.AllowedJumpers))
	for _, name := range p.AllowedJumpers {
		p.allowedJumpers[name] = true
	}
	for _, pattern := range p.TargetPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return Policy{}, fmt.Errorf("target_patterns 含非法正则 %q: %w", pattern, err)
		}
		p.targetRegexps = append(p.targetRegexps, re)
	}
	if err := p.validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p Policy) validate() error {
	if p.Version != 1 {
		return errors.New("version 必须为 1")
	}
	if p.IdleTimeoutSeconds <= 0 {
		return errors.New("idle_timeout_seconds 必须大于 0")
	}
	if p.MaxSessionSeconds <= 0 || p.MaxSessionSeconds < p.IdleTimeoutSeconds {
		return errors.New("max_session_seconds 必须大于 0 且不小于 idle_timeout_seconds")
	}
	if p.MaxOutputBytes <= 0 {
		return errors.New("max_output_bytes 必须大于 0")
	}
	return nil
}

// JumperAllowed 判断 jumper 是否在允许列表；空列表拒绝一切连接。
func (p Policy) JumperAllowed(name string) bool {
	return p.allowedJumpers[name]
}

// TargetAllowed 判断目标是否匹配允许模式；空目标（纯登录）放行，空模式列表拒绝一切目标。
func (p Policy) TargetAllowed(target string) bool {
	if target == "" {
		return true
	}
	for _, re := range p.targetRegexps {
		if re.MatchString(target) {
			return true
		}
	}
	return false
}

// expandTilde 把路径前缀 ~/ 展开为用户主目录。
func expandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// WriteExample 生成带注释的示例策略文件。
func WriteExample(path string) error {
	path = expandTilde(path)
	data := []byte(`# goto-mcp Agent 访问策略
# 策略描述「Agent 允许连接什么、做什么」；连接参数仍在 ~/.goto/config.yaml。
version: 1

# 允许 Agent 连接（session_open）的 jumper 列表；空列表拒绝所有连接。
allowed_jumpers:
  - prod-readonly

# 允许的目标主机正则列表；空列表拒绝所有非空目标。目标留空（纯登录跳板机）不受此限制。
# 注意：匹配的是规范化后的目标（host_arg_replace_dash 应用之后），如 app-01 会变成 app.01。
target_patterns:
  - "^app\\.[0-9]+$"
  - "^app\\.prod\\.[0-9]+$"

# v1 固定值，不可修改：
#   allow_session_input: false   —— Agent 无法向远程会话发送输入
#   host_key_policy: known_hosts_only —— 只信任 known_hosts，未知 host key 拒绝连接
#   credential_provider: os      —— 凭据只从系统安全存储读取
idle_timeout_seconds: 900
max_session_seconds: 1800
max_output_bytes: 65536
`)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
