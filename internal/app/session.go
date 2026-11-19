package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/pquerna/otp/totp"
	"golang.org/x/term"

	"goto/internal/termutil"
)

// sshArgs 根据所选 jumper 构造 ssh 命令行参数。
func sshArgs(jumper Jumper) []string {
	return []string{
		"-o", "IdentitiesOnly=yes",
		"-i", jumper.IdentityFile,
		"-p", fmt.Sprintf("%d", jumper.SSHPort),
		fmt.Sprintf("%s@%s", jumper.SSHUser, jumper.SSHHost),
	}
}

func run(jumper Jumper, cfg Config, target string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("需要在交互终端中运行")
	}
	cmd := exec.Command("ssh", sshArgs(jumper)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = ptmx.Close() }()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	resizeDone := make(chan struct{})
	go resizeOnSignal(ptmx, resizeDone)
	defer close(resizeDone)
	_ = pty.InheritSize(os.Stdin, ptmx)

	passwordRe := regexp.MustCompile(cfg.PasswordPrompt)
	hostKeyRe := regexp.MustCompile(cfg.HostKeyPromptRegex)
	otpRe := regexp.MustCompile(cfg.OTPPromptRegex)
	readerDone := make(chan error, 1)
	stdinDone := make(chan struct{})
	state := &sessionState{target: target}

	go func() {
		readerDone <- readAndRespond(ptmx, os.Stdout, jumper, cfg, state, passwordRe, hostKeyRe, otpRe)
	}()
	go func() {
		defer close(stdinDone)
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				// 密码等待期间输入路由给密码读取，其余透传给 ssh；
				// 统一由本 goroutine 独占读 stdin，避免与密码读取竞争。
				consumed, remaining := state.feedPassword(buf[:n])
				if !consumed {
					remaining = buf[:n]
				}
				if len(remaining) > 0 {
					_, _ = ptmx.Write(remaining)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	timer := time.AfterFunc(time.Duration(cfg.TimeoutSeconds)*time.Second, func() {
		if !state.sawAnyOutput() {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()

	select {
	case err := <-waitDone:
		return normalizeWaitError(err)
	case err := <-readerDone:
		if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
			_ = cmd.Process.Kill()
			return err
		}
		err = <-waitDone
		return normalizeWaitError(err)
	case <-stdinDone:
		err := <-waitDone
		return normalizeWaitError(err)
	}
}

type sessionState struct {
	mu           sync.Mutex
	passwordSent bool
	hostKeySent  bool
	otpSent      bool
	optionSent   bool
	sawOutput    bool
	target       string

	pwMu            sync.Mutex
	waitingPassword bool
	passwordCh      chan string
	passwordBuf     []byte
}

// markOutput 记录 ssh 端出现过输出，会话视为已建立。
func (s *sessionState) markOutput() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sawOutput = true
}

func (s *sessionState) sawAnyOutput() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sawOutput
}

// beginPasswordRead 让 stdin 转发 goroutine 把输入路由给密码读取，直到收到回车。
func (s *sessionState) beginPasswordRead() <-chan string {
	s.pwMu.Lock()
	defer s.pwMu.Unlock()
	s.waitingPassword = true
	s.passwordBuf = s.passwordBuf[:0]
	s.passwordCh = make(chan string, 1)
	return s.passwordCh
}

// feedPassword 把 stdin 输入喂给正在等待的密码读取。
// 返回 consumed（输入是否被密码读取消费）和 remaining（回车之后残留的字节，需透传给 ssh）。
// 密码等待结束后（等待期间收到 \r 或 \n 即结束），后续输入不再被消费。
func (s *sessionState) feedPassword(b []byte) (consumed bool, remaining []byte) {
	s.pwMu.Lock()
	defer s.pwMu.Unlock()
	if !s.waitingPassword {
		return false, nil
	}
	for i, c := range b {
		if c == '\r' || c == '\n' {
			s.passwordCh <- string(s.passwordBuf)
			s.passwordBuf = s.passwordBuf[:0]
			s.waitingPassword = false
			return true, b[i+1:]
		}
		s.passwordBuf = append(s.passwordBuf, c)
	}
	return true, nil
}

func readAndRespond(rw io.ReadWriter, out io.Writer, jumper Jumper, cfg Config, state *sessionState, passwordRe, hostKeyRe, otpRe *regexp.Regexp) error {
	buf := make([]byte, 1024)
	window := bytes.Buffer{}
	for {
		n, err := rw.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = out.Write(chunk)
			state.markOutput()
			appendWindow(&window, chunk)
			text := window.String()
			if err := maybeRespond(rw, jumper, cfg, state, passwordRe, hostKeyRe, otpRe, text); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

// appendWindow 维护用于提示匹配的滚动输出窗口：
// 超过上限时只保留尾部，且从完整行开始，避免把当前提示行拦腰截断。
func appendWindow(window *bytes.Buffer, chunk []byte) {
	window.Write(chunk)
	if window.Len() > 4096 {
		b := window.Bytes()
		keep := b[len(b)-2048:]
		if i := bytes.IndexByte(keep, '\n'); i >= 0 {
			keep = keep[i+1:]
		}
		window.Reset()
		window.Write(keep)
	}
}

func maybeRespond(w io.Writer, jumper Jumper, cfg Config, state *sessionState, passwordRe, hostKeyRe, otpRe *regexp.Regexp, text string) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if !state.hostKeySent && isCurrentLineMatch(hostKeyRe, text) {
		_, err := fmt.Fprint(w, "yes\r")
		if err != nil {
			return err
		}
		state.hostKeySent = true
		return nil
	}

	if !state.passwordSent && isCurrentLineMatch(passwordRe, text) {
		password := readPassword(state, "SSH password: ")
		_, err := fmt.Fprintf(w, "%s\r", password)
		if err != nil {
			return err
		}
		state.passwordSent = true
		return nil
	}

	if !state.otpSent && isCurrentLineMatch(otpRe, text) {
		if jumper.TOTPSecret == "" {
			return errors.New("需要 OTP，但未配置 totp_secret、GOTO_TOTP_SECRET 或 GOTO_TOTP_SECRET_<JUMPER>")
		}
		code, err := totp.GenerateCode(jumper.TOTPSecret, time.Now())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\r", code)
		if err != nil {
			return err
		}
		state.otpSent = true
		return nil
	}

	if !state.optionSent && state.target != "" && strings.Contains(termutil.StripControlSequences(termutil.CurrentOutputLine(text)), cfg.OptionPromptPattern) {
		_, err := fmt.Fprintf(w, "%s\r", state.target)
		if err != nil {
			return err
		}
		state.optionSent = true
		return nil
	}

	return nil
}

func isCurrentLineMatch(promptRe *regexp.Regexp, text string) bool {
	return termutil.CurrentLineMatches(promptRe, text)
}

// readPassword 通过统一 stdin 路由读取密码：
// stdin 由 run 的转发 goroutine 独占读取，密码等待期间输入被路由到这里。
func readPassword(state *sessionState, prompt string) string {
	ch := state.beginPasswordRead()
	_, _ = os.Stderr.WriteString("\r\n" + prompt)
	password := <-ch
	_, _ = os.Stderr.WriteString("\r\n")
	return password
}

func resizeOnSignal(ptmx *os.File, done <-chan struct{}) {
	ch := make(chan struct{}, 1)
	signalNotifyWindowChange(ch)
	for {
		select {
		case <-done:
			return
		case <-ch:
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}
}

func normalizeWaitError(err error) error {
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == syscall.SIGINT {
			return nil
		}
	}
	return err
}
