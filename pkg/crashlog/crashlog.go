// Package crashlog 提供独立于 zap 的崩溃日志管理器。
// panic 时 zap 可能不可用，本包直接用 os.File + JSON 写入，确保崩溃记录不丢失。
package crashlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── 崩溃记录 ──

// Record 表示一条崩溃日志记录（JSON Lines 格式写入文件）
type Record struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`       // "PANIC" | "FATAL"
	Component  string `json:"component"`   // 崩溃来源，如 "http.handler", "worker.login_audit"
	Error      string `json:"error"`       // recovered 值的字符串表示
	Goroutine  int    `json:"goroutine"`   // goroutine ID（尽力获取）
	StackTrace string `json:"stack_trace"` // 完整堆栈
	Recovered  bool   `json:"recovered"`   // 是否成功恢复（false 表示进程即将退出）
	PID        int    `json:"pid"`
	Uptime     string `json:"uptime"` // 进程运行时长
}

// ── 崩溃日志文件元数据 ──

// FileInfo 表示崩溃日志文件的元信息
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// ── CrashLogger ──

// Logger 是崩溃日志管理器，线程安全。
type Logger struct {
	dir       string
	maxFiles  int
	maxSize   int64
	startTime time.Time
	mu        sync.Mutex
}

// New 创建崩溃日志管理器。
//   - dir: 崩溃日志目录（自动创建）
//   - maxFiles: 最多保留文件数（<= 0 则默认 20）
//   - maxSize: 单文件最大字节（<= 0 则默认 50MB）
func New(dir string, maxFiles int, maxSize int64) *Logger {
	if maxFiles <= 0 {
		maxFiles = 20
	}
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024
	}
	_ = os.MkdirAll(dir, 0o755)
	return &Logger{
		dir:       dir,
		maxFiles:  maxFiles,
		maxSize:   maxSize,
		startTime: time.Now(),
	}
}

// Write 记录一条崩溃日志（component 标识来源，r 是 recover() 的返回值）。
// recovered 为 true 表示进程已恢复继续运行；为 false 表示即将退出。
func (l *Logger) Write(component string, r interface{}, recovered bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	stack := make([]byte, 8192)
	n := runtime.Stack(stack, false)
	stack = stack[:n]

	rec := Record{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Level:      "PANIC",
		Component:  component,
		Error:      fmt.Sprintf("%v", r),
		Goroutine:  extractGoroutineID(stack),
		StackTrace: string(stack),
		Recovered:  recovered,
		PID:        os.Getpid(),
		Uptime:     time.Since(l.startTime).Truncate(time.Second).String(),
	}
	if !recovered {
		rec.Level = "FATAL"
	}

	data, err := json.Marshal(rec)
	if err != nil {
		// 最后手段：直接写 stderr
		fmt.Fprintf(os.Stderr, "crashlog: marshal failed: %v\noriginal panic: %v\n%s\n", err, r, stack)
		return
	}

	filename := l.currentFilename()
	path := filepath.Join(l.dir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crashlog: open file failed: %v\n%s\n", err, data)
		return
	}
	defer f.Close()

	// 检查大小，超限则轮转
	info, _ := f.Stat()
	if info != nil && info.Size() > l.maxSize {
		f.Close()
		l.rotate(filename)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "crashlog: open rotated file failed: %v\n%s\n", err, data)
			return
		}
		defer f.Close()
	}

	data = append(data, '\n')
	_, _ = f.Write(data)

	// 同时输出到 stderr 方便控制台观察
	fmt.Fprintf(os.Stderr, "[CRASH] %s | %s | %s | recovered=%v\n%s\n", rec.Timestamp, rec.Component, rec.Error, rec.Recovered, rec.StackTrace)

	l.cleanup()
}

// ListFiles 列出所有崩溃日志文件（按修改时间倒序）
func (l *Logger) ListFiles() ([]FileInfo, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []FileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "crash-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}

// ReadFile 读取指定崩溃日志文件的内容
func (l *Logger) ReadFile(filename string) ([]byte, error) {
	// 安全校验：防止路径穿越
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, "/\\") {
		return nil, fmt.Errorf("invalid filename")
	}
	return os.ReadFile(filepath.Join(l.dir, filename))
}

// DeleteFile 删除指定崩溃日志文件
func (l *Logger) DeleteFile(filename string) error {
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, "/\\") {
		return fmt.Errorf("invalid filename")
	}
	return os.Remove(filepath.Join(l.dir, filename))
}

// Dir 返回崩溃日志目录路径
func (l *Logger) Dir() string {
	return l.dir
}

// ── 内部方法 ──

// currentFilename 返回当天的崩溃日志文件名
func (l *Logger) currentFilename() string {
	return fmt.Sprintf("crash-%s.log", time.Now().UTC().Format("2006-01-02"))
}

// rotate 将超大文件重命名（追加时间戳后缀）
func (l *Logger) rotate(filename string) {
	src := filepath.Join(l.dir, filename)
	base := strings.TrimSuffix(filename, ".log")
	dst := filepath.Join(l.dir, fmt.Sprintf("%s-%s.log", base, time.Now().UTC().Format("150405")))
	_ = os.Rename(src, dst)
}

// cleanup 清理超过 maxFiles 数量的旧文件
func (l *Logger) cleanup() {
	files, err := l.ListFiles()
	if err != nil || len(files) <= l.maxFiles {
		return
	}
	for _, f := range files[l.maxFiles:] {
		_ = os.Remove(filepath.Join(l.dir, f.Name))
	}
}

// extractGoroutineID 从堆栈文本中提取 goroutine ID（尽力而为）
func extractGoroutineID(stack []byte) int {
	// 堆栈格式: "goroutine 123 [running]:\n..."
	s := string(stack)
	if !strings.HasPrefix(s, "goroutine ") {
		return 0
	}
	s = s[len("goroutine "):]
	idx := strings.IndexByte(s, ' ')
	if idx <= 0 {
		return 0
	}
	var id int
	for _, c := range s[:idx] {
		if c < '0' || c > '9' {
			return 0
		}
		id = id*10 + int(c-'0')
	}
	return id
}
