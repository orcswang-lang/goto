package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
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
}

func defaultConfig() Config {
	return Config{
		SSHPort:             22,
		HostArgReplaceDash:  true,
		TimeoutSeconds:      30,
		PasswordPrompt:      "(?i)password:",
		HostKeyPromptRegex:  "(?i)yes/no",
		OTPPromptRegex:      "OTP Code.*:",
		OptionPromptPattern: "Opt",
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "goto.yaml"
	}
	return filepath.Join(home, ".goto", "config.yaml")
}

func writeExampleConfig(path string) error {
	cfg := defaultConfig()
	cfg.SSHUser = "your-user"
	cfg.SSHHost = "jumper.example.com"
	cfg.IdentityFile = "~/.ssh/private_key"

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadConfig(path string, cfg *Config) error {
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
	if cfg.TOTPSecret != "" {
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("配置文件 %s 包含 totp_secret 时权限必须为 0600 或更严格", path)
		}
	}
	return nil
}

func (c Config) validate() error {
	if c.SSHUser == "" {
		return errors.New("ssh_user 不能为空")
	}
	if c.SSHHost == "" {
		return errors.New("ssh_host 不能为空")
	}
	if c.SSHPort <= 0 || c.SSHPort > 65535 {
		return errors.New("ssh_port 必须在 1-65535 之间")
	}
	if c.IdentityFile == "" {
		return errors.New("identity_file 不能为空")
	}
	if c.TimeoutSeconds <= 0 {
		return errors.New("timeout_seconds 必须大于 0")
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
