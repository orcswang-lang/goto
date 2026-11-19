package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	legacyPasswordPrompt  = "(?i)password:"
	defaultPasswordPrompt = "(?i)(^|[^[:alnum:]_-])password:[[:space:]]*$"
	legacyJumperName      = "default"
)

// Jumper 描述一台跳板机的连接信息。
type Jumper struct {
	SSHUser      string `yaml:"ssh_user"`
	SSHHost      string `yaml:"ssh_host"`
	SSHPort      int    `yaml:"ssh_port"`
	IdentityFile string `yaml:"identity_file"`
	TOTPSecret   string `yaml:"totp_secret"`
}

// Config 是全局配置：多个 jumper 与共享的提示匹配、超时设置。
type Config struct {
	Default string            `yaml:"default"`
	Jumpers map[string]Jumper `yaml:"jumpers"`

	HostArgReplaceDash  bool   `yaml:"host_arg_replace_dash"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
	PasswordPrompt      string `yaml:"password_prompt"`
	HostKeyPromptRegex  string `yaml:"host_key_prompt_regex"`
	OTPPromptRegex      string `yaml:"otp_prompt_regex"`
	OptionPromptPattern string `yaml:"option_prompt_pattern"`
}

// DefaultConfig 返回全局默认配置。
func DefaultConfig() Config {
	return Config{
		Jumpers:             map[string]Jumper{},
		HostArgReplaceDash:  true,
		TimeoutSeconds:      30,
		PasswordPrompt:      defaultPasswordPrompt,
		HostKeyPromptRegex:  "(?i)yes/no",
		OTPPromptRegex:      "OTP Code.*:",
		OptionPromptPattern: "Opt",
	}
}

// UnmarshalYAML 兼容两种配置格式：
//   - 多 jumper 格式：顶层 jumpers map + default；
//   - 旧版扁平格式：顶层直接写 ssh_user/ssh_host 等，视为名为 default 的单个 jumper。
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	var fields map[string]yaml.Node
	if err := node.Decode(&fields); err != nil {
		return err
	}
	if _, multi := fields["jumpers"]; multi {
		for _, legacy := range []string{"ssh_user", "ssh_host", "ssh_port", "identity_file", "totp_secret"} {
			if _, ok := fields[legacy]; ok {
				return fmt.Errorf("顶层 %s 已过时：多 jumper 格式请把 SSH 连接字段移入 jumpers 下对应的 jumper", legacy)
			}
		}
		type plain Config
		var p plain
		if err := node.Decode(&p); err != nil {
			return err
		}
		// 未出现的顶层字段沿用默认值。
		if _, ok := fields["default"]; !ok {
			p.Default = c.Default
		}
		if _, ok := fields["host_arg_replace_dash"]; !ok {
			p.HostArgReplaceDash = c.HostArgReplaceDash
		}
		if _, ok := fields["timeout_seconds"]; !ok {
			p.TimeoutSeconds = c.TimeoutSeconds
		}
		if _, ok := fields["password_prompt"]; !ok {
			p.PasswordPrompt = c.PasswordPrompt
		}
		if _, ok := fields["host_key_prompt_regex"]; !ok {
			p.HostKeyPromptRegex = c.HostKeyPromptRegex
		}
		if _, ok := fields["otp_prompt_regex"]; !ok {
			p.OTPPromptRegex = c.OTPPromptRegex
		}
		if _, ok := fields["option_prompt_pattern"]; !ok {
			p.OptionPromptPattern = c.OptionPromptPattern
		}
		*c = Config(p)
		if c.Jumpers == nil {
			c.Jumpers = map[string]Jumper{}
		}
		return nil
	}

	legacy := struct {
		SSHUser             string `yaml:"ssh_user"`
		SSHHost             string `yaml:"ssh_host"`
		SSHPort             int    `yaml:"ssh_port"`
		IdentityFile        string `yaml:"identity_file"`
		TOTPSecret          string `yaml:"totp_secret"`
		HostArgReplaceDash  bool   `yaml:"host_arg_replace_dash"`
		TimeoutSeconds      int    `yaml:"timeout_seconds"`
		PasswordPrompt      string `yaml:"password_prompt"`
		HostKeyPromptRegex  string `yaml:"host_key_prompt_regex"`
		OTPPromptRegex      string `yaml:"otp_prompt_regex"`
		OptionPromptPattern string `yaml:"option_prompt_pattern"`
	}{
		HostArgReplaceDash:  c.HostArgReplaceDash,
		TimeoutSeconds:      c.TimeoutSeconds,
		PasswordPrompt:      c.PasswordPrompt,
		HostKeyPromptRegex:  c.HostKeyPromptRegex,
		OTPPromptRegex:      c.OTPPromptRegex,
		OptionPromptPattern: c.OptionPromptPattern,
	}
	if err := node.Decode(&legacy); err != nil {
		return err
	}
	c.HostArgReplaceDash = legacy.HostArgReplaceDash
	c.TimeoutSeconds = legacy.TimeoutSeconds
	c.PasswordPrompt = legacy.PasswordPrompt
	c.HostKeyPromptRegex = legacy.HostKeyPromptRegex
	c.OTPPromptRegex = legacy.OTPPromptRegex
	c.OptionPromptPattern = legacy.OptionPromptPattern
	c.Jumpers = map[string]Jumper{
		legacyJumperName: {
			SSHUser:      legacy.SSHUser,
			SSHHost:      legacy.SSHHost,
			SSHPort:      legacy.SSHPort,
			IdentityFile: legacy.IdentityFile,
			TOTPSecret:   legacy.TOTPSecret,
		},
	}
	if c.Default == "" {
		c.Default = legacyJumperName
	}
	return nil
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "goto.yaml"
	}
	return filepath.Join(home, ".goto", "config.yaml")
}

func WriteExampleConfig(path string) error {
	cfg := DefaultConfig()
	cfg.Default = "jumper-a"
	cfg.Jumpers = map[string]Jumper{
		"jumper-a": {
			SSHUser:      "your-user",
			SSHHost:      "jumper-a.example.com",
			SSHPort:      22,
			IdentityFile: "~/.ssh/private_key",
		},
		"jumper-b": {
			SSHUser:      "your-user",
			SSHHost:      "jumper-b.example.com",
			SSHPort:      22,
			IdentityFile: "~/.ssh/private_key_b",
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadConfig 从指定路径加载配置；文件不存在时保留默认值并返回 nil。
func LoadConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return err
	}
	if cfg.PasswordPrompt == legacyPasswordPrompt {
		cfg.PasswordPrompt = defaultPasswordPrompt
	}
	for name, jumper := range cfg.Jumpers {
		if jumper.SSHPort == 0 {
			jumper.SSHPort = 22
			cfg.Jumpers[name] = jumper
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
			for _, jumper := range cfg.Jumpers {
				if jumper.TOTPSecret != "" {
					return fmt.Errorf("配置文件 %s 包含 totp_secret 时权限必须为 0600 或更严格", path)
				}
			}
		}
	}
	return nil
}

func (j Jumper) Validate() error {
	if j.SSHUser == "" {
		return errors.New("ssh_user 不能为空")
	}
	if j.SSHHost == "" {
		return errors.New("ssh_host 不能为空")
	}
	if j.SSHPort <= 0 || j.SSHPort > 65535 {
		return errors.New("ssh_port 必须在 1-65535 之间")
	}
	if j.IdentityFile == "" {
		return errors.New("identity_file 不能为空")
	}
	return nil
}

func (c Config) Validate() error {
	if c.TimeoutSeconds <= 0 {
		return errors.New("timeout_seconds 必须大于 0")
	}
	if c.OptionPromptPattern == "" {
		return errors.New("option_prompt_pattern 不能为空")
	}
	if _, err := regexp.Compile(c.PasswordPrompt); err != nil {
		return fmt.Errorf("password_prompt 不是合法正则: %w", err)
	}
	if _, err := regexp.Compile(c.HostKeyPromptRegex); err != nil {
		return fmt.Errorf("host_key_prompt_regex 不是合法正则: %w", err)
	}
	if _, err := regexp.Compile(c.OTPPromptRegex); err != nil {
		return fmt.Errorf("otp_prompt_regex 不是合法正则: %w", err)
	}
	return nil
}

// SelectJumper 根据命令行指定的名称选出 jumper：
// 未指定时依次回退到 default 配置、唯一 jumper。
func (c Config) SelectJumper(name string) (string, Jumper, error) {
	if len(c.Jumpers) == 0 {
		return "", Jumper{}, errors.New("未配置任何 jumper")
	}
	if name == "" {
		name = c.Default
	}
	if name == "" {
		if len(c.Jumpers) == 1 {
			for n := range c.Jumpers {
				name = n
			}
		} else {
			return "", Jumper{}, fmt.Errorf("配置了多个 jumper，请用 -jumper 指定（可选: %s）", strings.Join(c.JumperNames(), ", "))
		}
	}
	jumper, ok := c.Jumpers[name]
	if !ok {
		return "", Jumper{}, fmt.Errorf("未找到 jumper %q（可选: %s）", name, strings.Join(c.JumperNames(), ", "))
	}
	return name, jumper, nil
}

func (c Config) JumperNames() []string {
	names := make([]string, 0, len(c.Jumpers))
	for name := range c.Jumpers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
