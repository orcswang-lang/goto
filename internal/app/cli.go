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
	var jumperName string
	var listJumpers bool
	var user string
	var host string
	var port int
	var identityFile string
	var totpSecret string
	var timeout int
	var showVersion bool

	flag.StringVar(&configPath, "config", defaultConfigPath(), "配置文件路径")
	flag.BoolVar(&initConfig, "init-config", false, "生成示例配置文件")
	flag.StringVar(&jumperName, "jumper", "", "选择使用的 jumper 名称（配置中 jumpers 的 key），不指定时使用 default 配置")
	flag.BoolVar(&listJumpers, "list", false, "列出所有 jumper")
	flag.StringVar(&user, "user", "", "SSH 用户名，覆盖所选 jumper 的配置")
	flag.StringVar(&host, "host", "", "SSH 主机，覆盖所选 jumper 的配置")
	flag.IntVar(&port, "port", 0, "SSH 端口，覆盖所选 jumper 的配置")
	flag.StringVar(&identityFile, "i", "", "SSH 私钥路径，覆盖所选 jumper 的配置")
	flag.StringVar(&totpSecret, "totp-secret", "", "TOTP Secret，优先使用环境变量 GOTO_TOTP_SECRET_<JUMPER> 或 GOTO_TOTP_SECRET")
	flag.IntVar(&timeout, "timeout", 0, "等待提示超时时间，单位秒")
	flag.BoolVar(&showVersion, "version", false, "显示版本")
	flag.Usage = usage
	os.Args = reorderArgs(flag.CommandLine, os.Args)
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

	if listJumpers {
		if len(cfg.Jumpers) == 0 {
			exitf("未配置任何 jumper")
		}
		for _, name := range cfg.jumperNames() {
			if name == cfg.Default {
				fmt.Printf("%s (默认)\n", name)
			} else {
				fmt.Println(name)
			}
		}
		return
	}

	name, jumper, err := cfg.selectJumper(jumperName)
	if err != nil {
		exitf("选择 jumper 失败: %v", err)
	}

	if user != "" {
		jumper.SSHUser = user
	}
	if host != "" {
		jumper.SSHHost = host
	}
	if port != 0 {
		jumper.SSHPort = port
	}
	if identityFile != "" {
		jumper.IdentityFile = identityFile
	}
	if timeout != 0 {
		cfg.TimeoutSeconds = timeout
	}
	switch {
	case os.Getenv(jumperTOTPEnvName(name)) != "":
		jumper.TOTPSecret = os.Getenv(jumperTOTPEnvName(name))
	case os.Getenv("GOTO_TOTP_SECRET") != "":
		jumper.TOTPSecret = os.Getenv("GOTO_TOTP_SECRET")
	case totpSecret != "":
		jumper.TOTPSecret = totpSecret
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
	if err := jumper.validate(); err != nil {
		exitf("jumper %q 配置无效: %v", name, err)
	}
	if err := run(jumper, cfg, target); err != nil {
		exitf("执行失败: %v", err)
	}
}

// jumperTOTPEnvName 返回 jumper 专属的 TOTP 环境变量名，
// 例如 "prod-a" -> "GOTO_TOTP_SECRET_PROD_A"。
func jumperTOTPEnvName(name string) string {
	var b strings.Builder
	b.WriteString("GOTO_TOTP_SECRET_")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// reorderArgs 把目标主机等位置参数移到所有选项之后。
// 标准库 flag 遇到第一个非选项参数就停止解析，导致
// `goto app-01 -jumper prod` 这类用法中的选项被忽略。
func reorderArgs(flags *flag.FlagSet, args []string) []string {
	positionals := make([]string, 0, len(args))
	options := make([]string, 0, len(args))
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, arg)
			positionals = append(positionals, args[i+1:]...)
			break
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(arg, "-"), "=")
		f := flags.Lookup(name)
		if !strings.HasPrefix(arg, "-") || arg == "-" || f == nil {
			positionals = append(positionals, arg)
			continue
		}
		options = append(options, arg)
		if hasValue || isBoolFlag(f) {
			continue
		}
		if i+1 < len(args) {
			i++
			options = append(options, args[i])
		}
	}
	return append([]string{args[0]}, append(options, positionals...)...)
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `用法:
  goto [target-host] [options]

示例:
  goto app-prod-01
  goto app-prod-01 -jumper prod
  goto app-prod-01 -config ~/.goto/config.yaml
  goto app-prod-01 -jumper prod -user your-user -host jumper.example.com -port 22 -i ~/.ssh/private_key
  goto -list
  goto -init-config

选项:
`)
	flag.PrintDefaults()
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "goto: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}
