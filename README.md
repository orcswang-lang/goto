# goto

`goto` 是一个用于快速登录跳板机并自动处理 MFA OTP 的命令行工具。它调用系统 `ssh`，通过 PTY 保持交互式终端体验，并支持 YAML 配置、多个跳板机（jumper）选择、命令行覆盖和环境变量注入敏感信息。

## 特性

- 单二进制运行，无需 Python 运行时
- 支持 YAML 配置文件，可配置多个 jumper 并随时切换
- 不带 `-jumper` 时交互式提示选择 jumper（终端环境）
- 支持命令行参数覆盖配置
- 支持 `GOTO_TOTP_SECRET_<JUMPER>` / `GOTO_TOTP_SECRET` 环境变量注入 TOTP Secret
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

编辑 `~/.goto/config.yaml`，在 `jumpers` 下为每台跳板机配置一项，`default` 指定不传 `-jumper` 时使用的跳板机：

```yaml
default: jumper-a
jumpers:
  jumper-a:
    ssh_user: your-user
    ssh_host: jumper-a.example.com
    ssh_port: 22
    identity_file: ~/.ssh/private_key
    totp_secret: ""
  jumper-b:
    ssh_user: your-user
    ssh_host: jumper-b.example.com
    ssh_port: 22
    identity_file: ~/.ssh/private_key_b
    totp_secret: ""
host_arg_replace_dash: true
timeout_seconds: 30
password_prompt: '(?i)(^|[^[:alnum:]_-])password:[[:space:]]*$'
host_key_prompt_regex: (?i)yes/no
otp_prompt_regex: 'OTP Code.*:'
option_prompt_pattern: Opt
```

推荐不要把 TOTP Secret 写入配置文件，改用每台跳板机独立的环境变量（jumper 名转大写、非字母数字替换为 `_`）：

```bash
export GOTO_TOTP_SECRET_JUMPER_A='jumper-a 的 TOTP Secret'
export GOTO_TOTP_SECRET_JUMPER_B='jumper-b 的 TOTP Secret'
```

如果只设置一个 `GOTO_TOTP_SECRET`，所有 jumper 共用它。

选择 jumper 并登录：

```bash
./goto app-prod-01 -jumper jumper-a
./goto app-prod-01 -jumper jumper-b
./goto app-prod-01            # 使用 default 指定的 jumper
```

不带 `-jumper` 时，如果配置了多个 jumper 且标准输入是终端，会交互式提示选择（支持编号、名称、回车选默认项）：

```text
$ goto
请选择 jumper:
  1) jumper-a
  2) jumper-b (默认)
输入编号或名称 [回车=默认]:
```

管道/脚本等非终端场景不会弹出选择提示：有 `default` 时直接用，无 `default` 时报错并提示使用 `-jumper`。goto 依赖交互终端运行 ssh 会话，非终端环境会明确报错退出。

列出所有 jumper：

```bash
./goto -list
```

如果 `host_arg_replace_dash` 为 `true`，`app-prod-01` 会在发送给跳板机选项时转换为 `app.prod.01`。

## 命令行参数

```bash
./goto [target-host] [options]
```

常用参数：

```bash
./goto app-prod-01 \
  -jumper jumper-a \
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

顶层字段：

| 字段                      | 说明                                            |
| ------------------------- | ----------------------------------------------- |
| `default`               | 默认 jumper 名称；仅配置一个 jumper 时可省略    |
| `jumpers`               | jumper 名到连接配置的映射                        |
| `host_arg_replace_dash` | 是否将目标参数中的`-` 替换为 `.`            |
| `timeout_seconds`       | 等待首个提示的超时时间                          |
| `password_prompt`       | 密码提示匹配正则；仅匹配当前行结尾的独立 password: |
| `host_key_prompt_regex` | SSH host key 确认提示匹配正则；仅匹配当前输出行 |
| `otp_prompt_regex`      | OTP 输入提示匹配正则；仅匹配当前输出行          |
| `option_prompt_pattern` | 跳板机目标选择提示匹配文本；仅匹配当前输出行    |

每个 jumper 的字段：

| 字段             | 说明                                                         |
| ---------------- | ------------------------------------------------------------ |
| `ssh_user`       | SSH 用户名                                                   |
| `ssh_host`       | 跳板机主机名或 IP                                            |
| `ssh_port`       | SSH 端口                                                     |
| `identity_file`  | SSH 私钥路径                                                 |
| `totp_secret`    | TOTP Secret，建议留空并使用 `GOTO_TOTP_SECRET_<JUMPER>` |

旧版扁平格式（顶层直接写 `ssh_user` 等字段）仍然兼容，会当作名为 `default` 的单个 jumper。

如果配置文件中任一 jumper 包含 `totp_secret`，文件权限必须为 `0600` 或更严格。

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
- 优先使用 `GOTO_TOTP_SECRET_<JUMPER>` / `GOTO_TOTP_SECRET` 注入 TOTP Secret
- 配置文件权限建议设置为 `0600`
- 私钥权限建议设置为 `0600`
- 如果历史提交中曾包含敏感信息，应及时轮换对应密钥或 Secret
