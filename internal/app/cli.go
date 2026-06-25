package app

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func Run(version string) {
	var configPath string
	var initConfig bool
	var user string
	var host string
	var port int
	var identityFile string
	var totpSecret string
	var timeout int
	var showVersion bool

	flag.StringVar(&configPath, "config", defaultConfigPath(), "配置文件路径")
	flag.BoolVar(&initConfig, "init-config", false, "生成示例配置文件")
	flag.StringVar(&user, "user", "", "SSH 用户名，覆盖配置文件")
	flag.StringVar(&host, "host", "", "SSH 主机，覆盖配置文件")
	flag.IntVar(&port, "port", 0, "SSH 端口，覆盖配置文件")
	flag.StringVar(&identityFile, "i", "", "SSH 私钥路径，覆盖配置文件")
	flag.StringVar(&totpSecret, "totp-secret", "", "TOTP Secret，优先使用环境变量 GOTO_TOTP_SECRET")
	flag.IntVar(&timeout, "timeout", 0, "等待提示超时时间，单位秒")
	flag.BoolVar(&showVersion, "version", false, "显示版本")
	flag.Usage = usage
	flag.Parse()

	if showVersion {
		fmt.Printf("goto %s\n", version)
		return
	}

	if initConfig {
		if err := writeExampleConfig(configPath); err != nil {
			exitf("生成配置失败: %v", err)
		}
		fmt.Printf("已生成配置: %s\n", configPath)
		return
	}

	cfg := defaultConfig()
	if err := loadConfig(configPath, &cfg); err != nil {
		exitf("读取配置失败: %v", err)
	}

	if user != "" {
		cfg.SSHUser = user
	}
	if host != "" {
		cfg.SSHHost = host
	}
	if port != 0 {
		cfg.SSHPort = port
	}
	if identityFile != "" {
		cfg.IdentityFile = identityFile
	}
	if timeout != 0 {
		cfg.TimeoutSeconds = timeout
	}
	if secret := os.Getenv("GOTO_TOTP_SECRET"); secret != "" {
		cfg.TOTPSecret = secret
	} else if totpSecret != "" {
		cfg.TOTPSecret = totpSecret
	}

	target := ""
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}
	if cfg.HostArgReplaceDash {
		target = strings.ReplaceAll(target, "-", ".")
	}

	if err := cfg.validate(); err != nil {
		exitf("配置无效: %v", err)
	}
	if err := run(cfg, target); err != nil {
		exitf("执行失败: %v", err)
	}
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `用法:
  goto [target-host] [options]

示例:
  goto app-prod-01
	goto app-prod-01 -config ~/.goto/config.yaml
	goto app-prod-01 -user your-user -host jumper.example.com -port 22 -i ~/.ssh/private_key
  goto -init-config

选项:
`)
	flag.PrintDefaults()
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
