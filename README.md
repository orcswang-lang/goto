# goto

`goto` 是一个用于快速登录跳板机并自动处理 MFA OTP 的命令行工具。它调用系统 `ssh`，通过 PTY 保持交互式终端体验，并支持 YAML 配置、命令行覆盖和环境变量注入敏感信息。

## 特性

- 单二进制运行，无需 Python 运行时
- 支持 YAML 配置文件
- 支持命令行参数覆盖配置
- 支持 `GOTO_TOTP_SECRET` 环境变量注入 TOTP Secret
- 自动识别并响应 SSH host key 确认、密码提示、OTP 提示、目标主机选择提示
- 支持 macOS、Linux、Windows 交叉构建
- 仓库内不硬编码真实账号、主机、私钥路径或 TOTP Secret

## 依赖

运行时依赖：

- 系统 `ssh` 命令
- 可用的私钥文件
- 如果启用 MFA，需提供 TOTP Secret

构建依赖：

- Go 1.23+

## 快速开始

生成配置文件：

```bash
./goto -init-config -config ~/.goto/config.yaml
chmod 600 ~/.goto/config.yaml
```

编辑 `~/.goto/config.yaml`：

```yaml
ssh_user: your-user
ssh_host: jumper.example.com
ssh_port: 22
identity_file: ~/.ssh/private_key
totp_secret: ""
host_arg_replace_dash: true
timeout_seconds: 30
password_prompt: '(?i)password:'
host_key_prompt_regex: (?i)yes/no
otp_prompt_regex: 'OTP Code.*:'
option_prompt_pattern: Opt
```

推荐不要把 TOTP Secret 写入配置文件，改用环境变量：

```bash
export GOTO_TOTP_SECRET='你的 TOTP Secret'
./goto app-prod-01
```

如果 `host_arg_replace_dash` 为 `true`，`app-prod-01` 会在发送给跳板机选项时转换为 `app.prod.01`。

## 命令行参数

```bash
./goto [target-host] [options]
```

常用参数：

```bash
./goto app-prod-01 \
  -config ~/.goto/config.yaml \
  -user your-user \
  -host jumper.example.com \
  -port 22 \
  -i ~/.ssh/private_key
```

查看帮助：

```bash
./goto -h
```

查看版本：

```bash
./goto -version
```

## 配置说明

| 字段                      | 说明                                            |
| ------------------------- | ----------------------------------------------- |
| `ssh_user`              | SSH 用户名                                      |
| `ssh_host`              | 跳板机主机名或 IP                               |
| `ssh_port`              | SSH 端口                                        |
| `identity_file`         | SSH 私钥路径                                    |
| `totp_secret`           | TOTP Secret，建议留空并使用`GOTO_TOTP_SECRET` |
| `host_arg_replace_dash` | 是否将目标参数中的`-` 替换为 `.`            |
| `timeout_seconds`       | 等待首个提示的超时时间                          |
| `password_prompt`       | 密码提示匹配正则                                |
| `host_key_prompt_regex` | SSH host key 确认提示匹配正则                   |
| `otp_prompt_regex`      | OTP 输入提示匹配正则                            |
| `option_prompt_pattern` | 跳板机目标选择提示匹配文本                      |

如果配置文件中包含 `totp_secret`，文件权限必须为 `0600` 或更严格。

## 构建

项目采用标准 Go 目录布局：

```text
cmd/goto/        命令行入口
internal/app/    配置、SSH 会话、平台适配等内部实现
scripts/         构建脚本
dist/            构建产物
```

本机开发构建：

```bash
go build -o goto ./cmd/goto
```

多平台构建：

```bash
chmod +x scripts/build.sh
VERSION=0.1.0 ./scripts/build.sh
```

产物会输出到 `dist/`：

- `goto-0.1.0-darwin-arm64`
- `goto-0.1.0-darwin-amd64`
- `goto-0.1.0-linux-amd64`
- `goto-0.1.0-linux-arm64`
- `goto-0.1.0-windows-amd64.exe`

## 安全建议

- 不要把真实 TOTP Secret、密码、私钥内容提交到仓库
- 优先使用 `GOTO_TOTP_SECRET` 注入 TOTP Secret
- 配置文件权限建议设置为 `0600`
- 私钥权限建议设置为 `0600`
- 如果历史提交中曾包含敏感信息，应及时轮换对应密钥或 Secret
