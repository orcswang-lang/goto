// goto-mcp 是 goto 面向其他 Agent 的 MCP 服务：
// 通过本地 stdio 提供只读跳板机会话能力（MCP tools）。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"goto/internal/agent"
	"goto/internal/app"
	"goto/internal/mcp"
	"goto/internal/policy"
)

var version = "dev"

func main() {
	var configPath string
	var policyPath string
	var initPolicy bool
	var showVersion bool

	flag.StringVar(&configPath, "config", app.DefaultConfigPath(), "goto 配置文件路径")
	flag.StringVar(&policyPath, "agent-policy", policy.DefaultPath(), "Agent 策略文件路径")
	flag.BoolVar(&initPolicy, "init-agent-policy", false, "生成示例 Agent 策略文件")
	flag.BoolVar(&showVersion, "version", false, "显示版本")
	flag.Parse()

	if showVersion {
		fmt.Printf("goto-mcp %s\n", version)
		return
	}

	if initPolicy {
		if err := policy.WriteExample(policyPath); err != nil {
			fmt.Fprintf(os.Stderr, "goto-mcp: 生成策略失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已生成 Agent 策略: %s\n", policyPath)
		return
	}

	// 日志只写 stderr；stdout 保留给 MCP 协议消息。
	logger := log.New(os.Stderr, "goto-mcp: ", log.LstdFlags)
	logf := logger.Printf

	server, err := agent.New(configPath, policyPath, version, logf)
	if err != nil {
		logf("%v", err)
		os.Exit(1)
	}
	defer server.Sessions.CloseAll()

	logf("启动: config=%s agent-policy=%s", configPath, policyPath)
	if err := mcp.Serve(os.Stdin, os.Stdout, server); err != nil {
		logf("服务退出: %v", err)
		os.Exit(1)
	}
}
