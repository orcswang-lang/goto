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
)

func run(cfg Config, target string) error {
	args := []string{
		"-o", "IdentitiesOnly=yes",
		"-i", cfg.IdentityFile,
		"-p", fmt.Sprintf("%d", cfg.SSHPort),
		fmt.Sprintf("%s@%s", cfg.SSHUser, cfg.SSHHost),
	}
	cmd := exec.Command("ssh", args...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = ptmx.Close() }()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
	defer func() { _ = pty.InheritSize(os.Stdin, ptmx) }()

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
		readerDone <- readAndRespond(ptmx, os.Stdout, cfg, state, passwordRe, hostKeyRe, otpRe)
	}()
	go func() {
		_, _ = io.Copy(ptmx, os.Stdin)
		close(stdinDone)
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	timer := time.AfterFunc(time.Duration(cfg.TimeoutSeconds)*time.Second, func() {
		if !state.seenAnyPrompt() {
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
	target       string
}

func (s *sessionState) seenAnyPrompt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.passwordSent || s.hostKeySent || s.otpSent || s.optionSent
}

func readAndRespond(rw io.ReadWriter, out io.Writer, cfg Config, state *sessionState, passwordRe, hostKeyRe, otpRe *regexp.Regexp) error {
	buf := make([]byte, 1024)
	window := bytes.Buffer{}
	for {
		n, err := rw.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = out.Write(chunk)
			window.Write(chunk)
			if window.Len() > 4096 {
				b := window.Bytes()
				window.Reset()
				window.Write(b[len(b)-2048:])
			}
			text := window.String()
			if err := maybeRespond(rw, cfg, state, passwordRe, hostKeyRe, otpRe, text); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

func maybeRespond(w io.Writer, cfg Config, state *sessionState, passwordRe, hostKeyRe, otpRe *regexp.Regexp, text string) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if !state.hostKeySent && hostKeyRe.MatchString(text) {
		_, err := fmt.Fprint(w, "yes\r")
		if err != nil {
			return err
		}
		state.hostKeySent = true
		return nil
	}

	if !state.passwordSent && passwordRe.MatchString(text) {
		password, err := readPassword("SSH password: ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\r", password)
		if err != nil {
			return err
		}
		state.passwordSent = true
		return nil
	}

	if !state.otpSent && otpRe.MatchString(text) {
		if cfg.TOTPSecret == "" {
			return errors.New("需要 OTP，但未配置 totp_secret 或 GOTO_TOTP_SECRET")
		}
		code, err := totp.GenerateCode(cfg.TOTPSecret, time.Now())
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

	if !state.optionSent && state.target != "" && strings.Contains(text, cfg.OptionPromptPattern) {
		_, err := fmt.Fprintf(w, "%s\r", state.target)
		if err != nil {
			return err
		}
		state.optionSent = true
		return nil
	}

	return nil
}

func readPassword(prompt string) (string, error) {
	_, _ = os.Stderr.WriteString("\r\n" + prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = os.Stderr.WriteString("\r\n")
	return string(password), err
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
