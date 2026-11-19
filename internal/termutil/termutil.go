// Package termutil 提供终端输出文本处理：控制序列剥离与提示匹配。
// CLI 与 MCP 会话共用，保证两者对远端输出的理解一致。
package termutil

import (
	"regexp"
	"strings"
)

// StripControlSequences 去除文本中的 ANSI 终端控制序列：
// CSI（\x1b[...X）、OSC（\x1b]...\a 或 \x1b\），其余 ESC 序列丢弃 ESC 与其后继字节。
func StripControlSequences(text string) string {
	var result strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '\x1b' {
			result.WriteByte(text[index])
			index++
			continue
		}

		if index+1 >= len(text) {
			break
		}
		switch text[index+1] {
		case '[':
			index += 2
			for index < len(text) {
				if text[index] >= 0x40 && text[index] <= 0x7e {
					index++
					break
				}
				index++
			}
		case ']':
			index += 2
			for index < len(text) && text[index] != '\a' {
				if text[index] == '\x1b' && index+1 < len(text) && text[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
			if index < len(text) && text[index] == '\a' {
				index++
			}
		default:
			index += 2
		}
	}
	return result.String()
}

// CurrentOutputLine 返回输出缓冲的当前行文本（最后一个换行符之后的内容）。
func CurrentOutputLine(text string) string {
	line := text[strings.LastIndexByte(text, '\n')+1:]
	line = strings.TrimRight(line, "\r")
	return line[strings.LastIndexByte(line, '\r')+1:]
}

// CurrentLineMatches 判断输出缓冲的当前行是否匹配提示正则。
func CurrentLineMatches(promptRe *regexp.Regexp, text string) bool {
	return promptRe.MatchString(StripControlSequences(CurrentOutputLine(text)))
}
