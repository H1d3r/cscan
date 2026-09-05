package scanner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// maxOutputBytes 单次 Execute 的 stdout/stderr 缓冲上限，防止大输出 OOM
const maxOutputBytes = 128 * 1024 * 1024 // 128MB

// maxConcurrentProcesses 全局信号量：限制所有扫描模块合计并发外部进程数。
// 无论 Worker 子任务并行度多少、各 scanner 内部 worker pool 多大，
// 实际运行的外部工具进程（naabu/nmap/nuclei/httpx/ffuf 等）总数 ≤ 此值。
const maxConcurrentProcesses = 5

var processSem = make(chan struct{}, maxConcurrentProcesses)

// acquireProcessSlot 获取全局进程槽位，支持 context 取消
func acquireProcessSlot(ctx context.Context) bool {
	select {
	case processSem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
func releaseProcessSlot() { <-processSem }

// runCommandContext executes a short-lived command while ensuring cancellation
// terminates the whole process group before the command returns.
func runCommandContext(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	setSysProcAttr(cmd)

	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessTree(cmd)
		case <-finished:
		}
	}()
	output, err := cmd.CombinedOutput()
	close(finished)
	return output, err
}

// cappedBuffer 带容量限制的 Buffer，超限后丢弃写入但返回成功以防管道阻塞
type cappedBuffer struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.maxBytes {
		c.truncated = true
		remaining := c.maxBytes - c.buf.Len()
		if remaining > 0 {
			c.buf.Write(p[:remaining])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}

// CmdExecutor 统一子进程执行器
type CmdExecutor struct {
	binaryPath     string
	memoryLimitMB  int64
	defaultTimeout time.Duration
	// presetArgs 每次执行自动前置注入的固定参数（如 -duc 禁用自动更新检查）
	presetArgs []string
}

func NewCmdExecutor(binaryPath string, memoryLimitMB int64, defaultTimeout time.Duration) *CmdExecutor {
	return &CmdExecutor{
		binaryPath:     binaryPath,
		memoryLimitMB:  memoryLimitMB,
		defaultTimeout: defaultTimeout,
		presetArgs:     presetArgsForBinary(binaryPath),
	}
}

// withPresetArgs 将固定注入参数前置到调用方参数之前
func (e *CmdExecutor) withPresetArgs(args []string) []string {
	if len(e.presetArgs) == 0 {
		return args
	}
	merged := make([]string, 0, len(e.presetArgs)+len(args))
	merged = append(merged, e.presetArgs...)
	merged = append(merged, args...)
	return merged
}

// CommandLine 返回实际执行的完整命令行（含自动注入的 preset 参数）。
// 每个 argv 均按 POSIX shell 规则引用，并在引用前按参数语义脱敏。
func (e *CmdExecutor) CommandLine(args []string) string {
	if e == nil {
		return ""
	}
	return FormatCommandLine(e.binaryPath, e.withPresetArgs(args))
}

// FormatCommandLine 将 binary 与 argv 格式化为可复现、可安全复制的 shell 命令。
// 它理解 flag/value、--flag=value、header 和 URL，不会把敏感值写入日志。
func FormatCommandLine(binary string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(redactURLSecrets(binary)))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flag, value, ok := splitFlagValue(arg); ok {
			if isHeaderFlag(flag) {
				arg = flag + "=" + redactHeaderValue(value)
			} else if isSensitiveFlag(flag) {
				arg = flag + "=<redacted>"
			} else {
				arg = flag + "=" + redactURLSecrets(value)
			}
		} else if strings.HasPrefix(arg, "-") && i+1 < len(args) {
			if isHeaderFlag(arg) {
				parts = append(parts, shellQuote(arg), shellQuote(redactHeaderValue(args[i+1])))
				i++
				continue
			}
			if isSensitiveFlag(arg) {
				parts = append(parts, shellQuote(arg), shellQuote("<redacted>"))
				i++
				continue
			}
		}
		arg = redactStandaloneSecrets(arg)
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func splitFlagValue(arg string) (string, string, bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", "", false
	}
	index := strings.IndexByte(arg, '=')
	if index <= 0 {
		return "", "", false
	}
	return arg[:index], arg[index+1:], true
}

func normalizedFlagName(flag string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimLeft(strings.TrimSpace(flag), "-")), "_", "-")
}

func isHeaderFlag(flag string) bool {
	trimmed := strings.TrimSpace(flag)
	if trimmed == "-H" {
		return true
	}
	switch normalizedFlagName(flag) {
	case "header", "headers":
		return true
	default:
		return false
	}
}

func isSensitiveName(name string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", "-")
	if normalized == "auth" || strings.HasPrefix(normalized, "auth-") || strings.HasSuffix(normalized, "-auth") {
		return true
	}
	for _, marker := range []string{
		"authorization", "api-key", "apikey", "token", "secret", "password", "passwd", "cookie",
		"credential", "signature", "access-key", "accesskey", "private-key", "privatekey",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "sig"
}

func isSensitiveFlag(flag string) bool {
	return isSensitiveName(normalizedFlagName(flag))
}

func redactHeaderValue(header string) string {
	name, value, found := strings.Cut(header, ":")
	if !found {
		if isSensitiveName(header) {
			return "<redacted>"
		}
		return redactURLSecrets(header)
	}
	if isSensitiveName(name) {
		return name + ": <redacted>"
	}
	return name + ":" + redactURLSecrets(value)
}

func redactStandaloneSecrets(arg string) string {
	if schemeEnd := strings.Index(arg, "://"); schemeEnd > 0 && !strings.ContainsAny(arg[:schemeEnd], " \t:") {
		return redactURLSecrets(arg)
	}
	if key, _, found := strings.Cut(arg, "="); found && isSensitiveName(strings.TrimLeft(key, "-")) {
		return key + "=<redacted>"
	}
	name, _, found := strings.Cut(arg, ":")
	if found {
		headerName := strings.TrimPrefix(name, "-H")
		if isSensitiveName(headerName) {
			return name + ": <redacted>"
		}
	}
	return redactURLSecrets(arg)
}

func redactURLSecrets(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.Contains(trimmed, "://") {
		return raw
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<redacted-url>"
	}
	if parsed.User != nil {
		parsed.User = url.User("<redacted>")
	}
	query := parsed.Query()
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if isSensitiveName(key) {
			query[key] = []string{"<redacted>"}
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (e *CmdExecutor) Execute(ctx context.Context, args []string, opts ExecuteOpts) (*ExecuteResult, error) {
	result := &ExecuteResult{LogFn: opts.LogFn}

	// 全局信号量：限制并发外部进程数
	if !acquireProcessSlot(ctx) {
		result.Stderr = "canceled while waiting for process slot"
		return result, ctx.Err()
	}
	defer releaseProcessSlot()

	args = e.withPresetArgs(args)

	timeout := e.defaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.binaryPath, args...)
	setSysProcAttr(cmd)
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}

	stdoutBuf := &cappedBuffer{maxBytes: maxOutputBytes}
	stderrBuf := &cappedBuffer{maxBytes: maxOutputBytes}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	startTime := time.Now()
	err := cmd.Start()
	if err != nil {
		result.Stderr = fmt.Sprintf("failed to start: %v", err)
		return result, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-execCtx.Done():
		killProcessTree(cmd)
		<-done
		result.Duration = time.Since(startTime)
		result.Stdout = stdoutBuf.String()
		result.Stderr = stderrBuf.String()
		if stdoutBuf.truncated {
			result.Stderr += fmt.Sprintf("\n[stdout truncated at %d bytes]", maxOutputBytes)
		}
		if ctx.Err() != nil {
			return result, fmt.Errorf("%s: execution canceled: %w", e.binaryPath, ctx.Err())
		}
		return result, fmt.Errorf("%s: timeout after %v: %w", e.binaryPath, timeout, context.DeadlineExceeded)
	case err := <-done:
		result.Stdout = stdoutBuf.String()
		result.Stderr = stderrBuf.String()
		result.ExitCode = 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			}
		}
		result.Duration = time.Since(startTime)
		if stdoutBuf.truncated {
			if result.LogFn != nil {
				result.LogFn("WARN", "[%s] stdout truncated at %d bytes", e.binaryPath, maxOutputBytes)
			} else {
				logx.Infof("[%s] stdout truncated at %d bytes", e.binaryPath, maxOutputBytes)
			}
		}
		return result, err
	}
}

func (e *CmdExecutor) StreamLines(ctx context.Context, args []string, handler func(line string) (bool, error), opts ExecuteOpts) error {
	// 全局信号量：限制并发外部进程数
	if !acquireProcessSlot(ctx) {
		return ctx.Err()
	}
	defer releaseProcessSlot()

	args = e.withPresetArgs(args)

	timeout := e.defaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.binaryPath, args...)
	setSysProcAttr(cmd)
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		stdoutPipe.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdoutPipe.Close()
		stderrPipe.Close()
		return fmt.Errorf("start: %w", err)
	}

	// Context watcher: on cancellation, kill process tree promptly to unblock scanner.Scan()
	go func() {
		<-execCtx.Done()
		killProcessTree(cmd)
	}()

	done := make(chan error, 1)
	go func() {
		var stderrBuf bytes.Buffer
		_, _ = io.Copy(&stderrBuf, stderrPipe)
		stderrPipe.Close()
		err := cmd.Wait()
		if err != nil && stderrBuf.Len() > 0 {
			logx.Debugf("[%s] command failed with %d stderr bytes", e.binaryPath, stderrBuf.Len())
		}
		done <- err
	}()

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		cont, err := handler(line)
		if err != nil {
			killProcessTree(cmd)
			<-done
			stdoutPipe.Close()
			return fmt.Errorf("handler error: %w", err)
		}
		if !cont {
			killProcessTree(cmd)
			break
		}
	}

	stdoutPipe.Close()
	cmdErr := <-done
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdout: %w", err)
	}
	if cmdErr != nil {
		if err := execCtx.Err(); err != nil {
			return err
		}
		return cmdErr
	}
	return nil
}

func (e *CmdExecutor) CheckHealth() ToolHealth {
	health := ToolHealth{Name: e.binaryPath}

	path, err := exec.LookPath(e.binaryPath)
	if err != nil {
		health.Available = false
		health.Error = fmt.Sprintf("binary not found: %v", err)
		return health
	}

	health.Available = true
	health.Path = path

	cmd := exec.Command(e.binaryPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		health.Version = "unknown"
		return health
	}
	health.Version = parseVersion(string(output))
	return health
}

func parseVersion(output string) string {
	fields := strings.Fields(output)
	for _, f := range fields {
		if looksLikeVersion(f) {
			return f
		}
	}
	return "unknown"
}

func looksLikeVersion(s string) bool {
	if len(s) < 2 || len(s) > 20 {
		return false
	}
	hasDigit := false
	hasDot := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if c == '.' {
			hasDot = true
		}
	}
	return hasDigit && hasDot
}

// ExecuteOpts 执行选项
type ExecuteOpts struct {
	Timeout       time.Duration
	MemoryLimitMB int64
	WorkingDir    string
	Env           []string
	// LogFn 可选的任务日志回调，设置后 Execute 内部日志和 LogResult 将通过此回调输出
	LogFn func(level, format string, args ...interface{}) `json:"-"`
}

// LogResult 记录命令执行结果（Debug 级别，用于排障）
func (e *CmdExecutor) LogResult(prefix string, result *ExecuteResult, err error) {
	logFn := result.LogFn
	if logFn == nil {
		logFn = func(level, format string, args ...interface{}) {
			switch level {
			case "ERROR":
				logx.Errorf(format, args...)
			case "WARN":
				logx.Slowf(format, args...)
			case "INFO":
				logx.Infof(format, args...)
			default:
				logx.Debugf(format, args...)
			}
		}
	}
	logFn("DEBUG", "[%s] %s: exit=%d duration=%s stdout_bytes=%d stderr_bytes=%d err=%v",
		e.binaryPath, prefix, result.ExitCode, result.Duration, len(result.Stdout), len(result.Stderr), err)
	// Do not emit command stdout/stderr contents here: scanner output can contain
	// response bodies, headers, cookies, credentials, or template material.
}

// ExecuteResult 执行结果
type ExecuteResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	// LogFn 从 ExecuteOpts 透传的任务日志回调
	LogFn func(level, format string, args ...interface{}) `json:"-"`
}

// ToolHealth 工具健康检查结果
type ToolHealth struct {
	Name      string
	Available bool
	Path      string
	Version   string
	Error     string
}
