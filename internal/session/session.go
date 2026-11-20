// Package session 实现 goto-mcp 使用的受控 SSH/PTY 会话：
// 严格 known_hosts、凭据来自本地安全存储、只读、可超时回收。
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/pquerna/otp/totp"

	"goto/internal/termutil"
)

// State 是会话状态。
type State string

const (
	StateConnecting State = "connecting"
	StateReady      State = "ready"
	StateFailed     State = "failed"
	StateExpired    State = "expired"
	StateClosed     State = "closed"
)

// Credentials 提供会话所需的凭据；调用方注入，会话不关心存储位置。
type Credentials interface {
	GetPassword(jumper string) (string, error)
	GetTOTPSecret(jumper string) (string, error)
}

// Options 是会话连接参数。
type Options struct {
	SSHCommand     string // 默认 "ssh"；仅测试注入使用
	User           string
	Host           string
	Port           int
	IdentityFile   string
	Target         string // 跳板机目标主机选择；为空表示纯登录
	Jumper         string // 凭据查找键（jumper 名）
	Credentials    Credentials
	PasswordPrompt string
	HostKeyPrompt  string
	OTPPrompt      string
	OptionPrompt   string
	LoginTimeout   time.Duration // 等待 ssh 首次输出的上限
	IdleTimeout    time.Duration // 空闲超时
	MaxLifetime    time.Duration // 会话最大生命周期
	MaxOutput      int           // 可见输出缓冲上限（字节）
}

// ReadResult 是一次读取的结果。
type ReadResult struct {
	State        State
	Chunk        string
	NextCursor   int
	Truncated    bool // 输出缓冲已因超出上限丢弃最旧内容
	ErrorCode    string
	ErrorMessage string
}

// Session 是一个受控 SSH/PTY 会话。
// shortcut: 凭据获取在输出处理锁内同步完成；本地安全存储读取快，
// 若后端需要交互解锁导致阻塞，Read 会一并阻塞，升级时改为异步取凭据。
type Session struct {
	id     string
	opt    Options
	creds  Credentials
	cmd    *exec.Cmd
	ptmx   *os.File
	passRe *regexp.Regexp
	hostRe *regexp.Regexp
	otpRe  *regexp.Regexp

	mu           sync.Mutex
	state        State
	errCode      string
	errMsg       string
	visible      string // 剥离控制序列、脱敏后的输出
	base         int    // visible 首字节的绝对偏移（已丢弃字节数）
	returned     int    // visible 内已返回长度
	secrets      []string
	passwordSent bool
	otpSent      bool
	optionSent   bool
	hostKeySent  bool
	sawOutput    bool
	createdAt    time.Time
	lastActive   time.Time
	newOutput    chan struct{}
	done         chan struct{}
}

// Open 启动 ssh 并返回会话；返回时状态为 connecting，登录过程在后台推进。
func Open(opt Options) (*Session, error) {
	if opt.SSHCommand == "" {
		opt.SSHCommand = "ssh"
	}
	if opt.User == "" || opt.Host == "" || opt.IdentityFile == "" {
		return nil, errors.New("ssh 连接参数不完整")
	}
	if opt.Port <= 0 || opt.Port > 65535 {
		return nil, errors.New("ssh 端口无效")
	}
	if opt.Credentials == nil {
		return nil, errors.New("凭据提供器不能为空")
	}
	if opt.LoginTimeout <= 0 {
		opt.LoginTimeout = 30 * time.Second
	}
	if opt.MaxOutput <= 0 {
		opt.MaxOutput = 64 * 1024
	}
	var err error
	if opt.PasswordPrompt == "" {
		return nil, errors.New("password_prompt 不能为空")
	}
	if opt.HostKeyPrompt == "" || opt.OTPPrompt == "" || opt.OptionPrompt == "" {
		return nil, errors.New("提示匹配配置不能为空")
	}

	passRe, err := regexp.Compile(opt.PasswordPrompt)
	if err != nil {
		return nil, fmt.Errorf("password_prompt 非法: %w", err)
	}
	hostRe, err := regexp.Compile(opt.HostKeyPrompt)
	if err != nil {
		return nil, fmt.Errorf("host_key_prompt 非法: %w", err)
	}
	otpRe, err := regexp.Compile(opt.OTPPrompt)
	if err != nil {
		return nil, fmt.Errorf("otp_prompt 非法: %w", err)
	}

	cmd := exec.Command(opt.SSHCommand, sshArgs(opt)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	id := newID()
	s := &Session{
		id:         id,
		opt:        opt,
		creds:      opt.Credentials,
		cmd:        cmd,
		ptmx:       ptmx,
		passRe:     passRe,
		hostRe:     hostRe,
		otpRe:      otpRe,
		state:      StateConnecting,
		createdAt:  time.Now(),
		lastActive: time.Now(),
		newOutput:  make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	go s.readLoop()
	go s.expiryLoop()
	return s, nil
}

// ID 返回会话标识。
func (s *Session) ID() string { return s.id }

// State 返回当前状态。
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Read 返回会话的新输出；游标由服务端权威维护，客户端仅用于顺序断言。
func (s *Session) Read(maxBytes int, wait time.Duration) ReadResult {
	if maxBytes <= 0 {
		maxBytes = 16 * 1024
	}
	deadline := time.Now().Add(wait)
	s.mu.Lock()
	for !s.terminal() && s.returned >= len(s.visible) {
		remain := time.Until(deadline)
		if remain <= 0 {
			break
		}
		s.mu.Unlock()
		select {
		case <-s.newOutput:
		case <-s.done:
		case <-time.After(remain):
		}
		s.mu.Lock()
	}
	defer s.mu.Unlock()
	end := s.returned + maxBytes
	if end > len(s.visible) {
		end = len(s.visible)
	}
	chunk := s.visible[s.returned:end]
	s.returned = end
	return ReadResult{
		State:        s.state,
		Chunk:        chunk,
		NextCursor:   s.base + s.returned,
		Truncated:    s.base > 0,
		ErrorCode:    s.errCode,
		ErrorMessage: s.errMsg,
	}
}

// Close 关闭会话；幂等，会等待后台循环退出，并释放凭据与输出缓冲。
func (s *Session) Close() {
	s.mu.Lock()
	if s.state == StateClosed {
		s.mu.Unlock()
		return
	}
	s.state = StateClosed
	s.killLocked()
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	s.secrets = nil
	s.visible = ""
	s.returned = 0
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

func (s *Session) terminal() bool {
	switch s.state {
	case StateFailed, StateExpired, StateClosed:
		return true
	}
	return false
}

func (s *Session) killLocked() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *Session) failLocked(code, msg string) {
	if s.terminal() {
		return
	}
	s.state = StateFailed
	s.errCode = code
	s.errMsg = msg
	s.killLocked()
}

func (s *Session) expireLocked() {
	if s.terminal() {
		return
	}
	s.state = StateExpired
	s.errCode = "SESSION_EXPIRED"
	s.errMsg = "会话已因空闲或超过最大生命周期而关闭"
	s.killLocked()
}

func (s *Session) markOutputLocked() {
	s.sawOutput = true
	s.lastActive = time.Now()
	if s.state == StateConnecting {
		s.state = StateReady
	}
}

// appendVisibleLocked 追加可见输出：剥离控制序列、脱敏、按上限截断。
func (s *Session) appendVisibleLocked(chunk []byte) {
	text := termutil.StripControlSequences(string(chunk))
	for _, secret := range s.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "********")
		}
	}
	s.visible += text
	if len(s.visible) > s.opt.MaxOutput {
		drop := len(s.visible) - s.opt.MaxOutput
		// 修正到 rune 边界，避免切开多字节 UTF-8。
		for drop < len(s.visible) && !utf8.RuneStart(s.visible[drop]) {
			drop++
		}
		keep := s.visible[drop:]
		if i := strings.IndexByte(keep, '\n'); i >= 0 {
			drop += i + 1
			keep = keep[i+1:]
		}
		s.visible = keep
		s.base += drop
		if s.returned > drop {
			s.returned -= drop
		} else {
			s.returned = 0
		}
	}
}

func (s *Session) signalNewOutput() {
	select {
	case s.newOutput <- struct{}{}:
	default:
	}
}

// readLoop 独占读取 PTY 输出并驱动提示应答；退出时关闭 done。
func (s *Session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.appendVisibleLocked(buf[:n])
			s.markOutputLocked()
			s.maybeRespondLocked()
			s.signalNewOutput()
			s.mu.Unlock()
		}
		if err != nil {
			werr := s.cmd.Wait()
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.terminal() {
				return
			}
			if werr == nil {
				s.state = StateClosed
			} else if strings.Contains(s.visible, "Host key verification failed") ||
				strings.Contains(s.visible, "REMOTE HOST IDENTIFICATION HAS CHANGED") {
				// StrictHostKeyChecking=yes 下 ssh 对未知 key 直接报错退出，不会弹出 yes/no。
				s.state = StateFailed
				s.errCode = "HOST_KEY_REQUIRED"
				s.errMsg = "目标主机不在 known_hosts 信任列表中，连接被 ssh 拒绝；请先在人工会话中确认该 host key"
			} else {
				s.state = StateFailed
				s.errCode = "UPSTREAM_ERROR"
				s.errMsg = "远端会话异常退出"
			}
			return
		}
	}
}

// maybeRespondLocked 处理远端提示；要求持有 s.mu。
// host key 提示一律拒绝：v1 只信任 known_hosts，未知 key 不允许 Agent 代为确认。
// 所有应答仅限登录窗口（LoginTimeout）内：登录完成后远端输出的类似提示文本
// （如应用自身的 password: 提示）不得再触发凭据注入。
func (s *Session) maybeRespondLocked() {
	if s.terminal() {
		return
	}
	if time.Since(s.createdAt) > s.opt.LoginTimeout {
		return
	}
	text := s.visible
	if !s.hostKeySent && termutil.CurrentLineMatches(s.hostRe, text) {
		s.hostKeySent = true
		_, _ = s.ptmx.WriteString("no\r")
		s.failLocked("HOST_KEY_REQUIRED", "目标主机不在 known_hosts 信任列表中，已拒绝连接；请先在人工会话中确认该 host key")
		return
	}
	if !s.passwordSent && termutil.CurrentLineMatches(s.passRe, text) {
		password, err := s.creds.GetPassword(s.opt.Jumper)
		if err != nil {
			s.failLocked("AUTH_REQUIRED", "系统安全凭据存储中没有该 jumper 的密码，无法自动登录")
			return
		}
		s.passwordSent = true
		s.secrets = append(s.secrets, password)
		_, _ = s.ptmx.WriteString(password + "\r")
		return
	}
	if !s.otpSent && termutil.CurrentLineMatches(s.otpRe, text) {
		secret, err := s.creds.GetTOTPSecret(s.opt.Jumper)
		if err != nil {
			s.failLocked("AUTH_REQUIRED", "系统安全凭据存储中没有该 jumper 的 TOTP Secret，无法完成 MFA")
			return
		}
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			s.failLocked("AUTH_REQUIRED", "TOTP Secret 无效，无法生成验证码")
			return
		}
		s.otpSent = true
		s.secrets = append(s.secrets, code)
		_, _ = s.ptmx.WriteString(code + "\r")
		return
	}
	if !s.optionSent && s.opt.Target != "" &&
		strings.Contains(termutil.StripControlSequences(termutil.CurrentOutputLine(text)), s.opt.OptionPrompt) {
		s.optionSent = true
		_, _ = s.ptmx.WriteString(s.opt.Target + "\r")
		return
	}
}

// expiryLoop 定期检查登录超时、空闲超时与最大生命周期。
func (s *Session) expiryLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			switch s.state {
			case StateConnecting, StateReady:
				now := time.Now()
				switch {
				case !s.sawOutput && now.Sub(s.createdAt) > s.opt.LoginTimeout:
					s.failLocked("TIMEOUT", "等待 ssh 首次输出超时")
				case s.sawOutput && now.Sub(s.lastActive) > s.opt.IdleTimeout:
					s.expireLocked()
				case now.Sub(s.createdAt) > s.opt.MaxLifetime:
					s.expireLocked()
				}
			}
			s.mu.Unlock()
		}
	}
}

// sshArgs 构造 ssh 参数；StrictHostKeyChecking=yes 实现 known_hosts_only 策略。
func sshArgs(o Options) []string {
	return []string{
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-i", o.IdentityFile,
		"-p", strconv.Itoa(o.Port),
		o.User + "@" + o.Host,
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Manager 管理全部会话。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager 返回会话管理器。
func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// Add 登记会话。
func (m *Manager) Add(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.id] = s
}

// Get 按 id 取会话。
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Remove 删除会话登记；会话本身需先 Close。
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// CloseAll 关闭全部会话（服务退出时调用）。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}
