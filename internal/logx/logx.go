// Package logx provides the portable launcher's logging facility.
//
// Logs are written to <usb>/logs/opencode-portable.log and rotated once they
// grow beyond a size threshold. Nothing that could contain credentials is
// ever logged: the only values logged are paths, versions, detection results,
// selection decisions and error messages.
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Level is a log severity level.
type Level int

const (
	// LevelDebug logs everything.
	LevelDebug Level = iota
	// LevelInfo logs normal operational messages.
	LevelInfo
	// LevelWarn logs recoverable problems.
	LevelWarn
	// LevelError logs failures.
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "?"
}

const (
	maxLogSize    = 1 << 20 // 1 MiB per log file before rotation
	maxLogFiles   = 3       // keep this many rotated files
	logFileName   = "opencode-portable.log"
	rotatedPrefix = "opencode-portable"
)

// Logger writes leveled log lines to a rotating file, optionally mirroring
// them to stderr when verbose mode is enabled.
type Logger struct {
	mu      sync.Mutex
	dir     string
	level   Level
	verbose bool
	file    *os.File
}

// New creates a Logger rooted at dir (the USB logs directory). The directory
// is created if possible. If the file cannot be opened, logging silently
// degrades to stderr-only (never crashes the launcher).
func New(dir string, level Level, verbose bool) *Logger {
	l := &Logger{dir: dir, level: level, verbose: verbose}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.openLocked()
	return l
}

func (l *Logger) openLocked() {
	if l.dir == "" {
		return
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		l.dir = ""
		return
	}
	f, err := os.OpenFile(filepath.Join(l.dir, logFileName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		l.dir = ""
		return
	}
	l.file = f
	l.rotateIfNeededLocked()
}

func (l *Logger) rotateIfNeededLocked() {
	if l.file == nil {
		return
	}
	st, err := l.file.Stat()
	if err != nil || st.Size() < maxLogSize {
		return
	}
	_ = l.file.Close()
	l.file = nil
	now := time.Now()
	old := filepath.Join(l.dir, fmt.Sprintf("%s.%s.log", rotatedPrefix, now.Format("20060102-150405")))
	_ = os.Rename(filepath.Join(l.dir, logFileName), old)
	// Trim old rotations.
	matches, _ := filepath.Glob(filepath.Join(l.dir, rotatedPrefix+".*.log"))
	if len(matches) > maxLogFiles {
		sort.Strings(matches)
		for _, m := range matches[:len(matches)-maxLogFiles] {
			_ = os.Remove(m)
		}
	}
	l.openLocked()
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

// SetVerbose toggles stderr mirroring.
func (l *Logger) SetVerbose(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.verbose = v
}

// Debug logs at debug level.
func (l *Logger) Debug(format string, args ...any) { l.log(LevelDebug, format, args...) }

// Info logs at info level.
func (l *Logger) Info(format string, args ...any) { l.log(LevelInfo, format, args...) }

// Warn logs at warn level.
func (l *Logger) Warn(format string, args ...any) { l.log(LevelWarn, format, args...) }

// Error logs at error level.
func (l *Logger) Error(format string, args ...any) { l.log(LevelError, format, args...) }

func (l *Logger) log(level Level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if level < l.level {
		return
	}
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format(time.RFC3339), level, Sanitize(fmt.Sprintf(format, args...)))
	if l.verbose {
		_, _ = io.WriteString(os.Stderr, line)
	}
	if l.file == nil {
		return
	}
	_, _ = io.WriteString(l.file, line)
	_ = l.file.Sync()
}

// sanitizePatterns are compiled once; Sanitize is applied to every log line
// as defense in depth against accidental credential leakage.
var sanitizePatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`Bearer [A-Za-z0-9._-]{12,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`api[_-]?key["']?\s*[:=]\s*["']?[A-Za-z0-9._-]{12,}`),
}

// Sanitize removes common secret patterns from a message. It is defensive:
// callers should avoid logging sensitive material entirely, but this catches
// accidental leakage of common credential formats.
func Sanitize(s string) string {
	for _, re := range sanitizePatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}
