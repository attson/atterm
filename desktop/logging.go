package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultLogPreviewBytes = 256 * 1024
	defaultLogMaxBytes     = 10 * 1024 * 1024
	defaultLogMaxBackups   = 5
)

type loggingConfigState struct {
	enabled bool
	path    string
}

type logPreview struct {
	Path      string
	Exists    bool
	Truncated bool
	Content   string
}

type loggingOptions struct {
	devMode       bool
	terminal      io.Writer
	maxBytes      int64
	maxBackups    int
	defaultPathFn func() string
}

type loggingManager struct {
	mu            sync.Mutex
	logger        *log.Logger
	devMode       bool
	terminal      io.Writer
	maxBytes      int64
	maxBackups    int
	defaultPathFn func() string

	currentPath   string
	currentWriter io.Writer
	currentCloser io.Closer
}

func newLoggingManager(opts loggingOptions) (*loggingManager, error) {
	if opts.terminal == nil {
		opts.terminal = io.Discard
	}
	if opts.maxBytes <= 0 {
		opts.maxBytes = defaultLogMaxBytes
	}
	if opts.maxBackups <= 0 {
		opts.maxBackups = defaultLogMaxBackups
	}
	if opts.defaultPathFn == nil {
		opts.defaultPathFn = defaultLogFilePath
	}

	m := &loggingManager{
		devMode:       opts.devMode,
		terminal:      opts.terminal,
		maxBytes:      opts.maxBytes,
		maxBackups:    opts.maxBackups,
		defaultPathFn: opts.defaultPathFn,
		currentWriter: io.Discard,
	}
	m.logger = log.New(m, "", log.LstdFlags)
	log.SetOutput(m)
	return m, nil
}

func (m *loggingManager) Logger() *log.Logger {
	return m.logger
}

func (m *loggingManager) EffectivePath(path string) string {
	if path != "" {
		return path
	}
	return m.defaultPathFn()
}

func (m *loggingManager) Apply(cfg loggingConfigState) error {
	nextWriter := io.Writer(io.Discard)
	var (
		nextCloser io.Closer
		nextPath   string
	)

	if cfg.enabled {
		nextPath = m.EffectivePath(cfg.path)
		if err := os.MkdirAll(filepath.Dir(nextPath), 0o755); err != nil {
			return err
		}
		fileWriter, err := newRotatingFileWriter(nextPath, m.maxBytes, m.maxBackups)
		if err != nil {
			return err
		}
		nextCloser = fileWriter
		if m.devMode {
			nextWriter = io.MultiWriter(m.terminal, fileWriter)
		} else {
			nextWriter = fileWriter
		}
	} else if m.devMode {
		nextWriter = m.terminal
	}

	m.mu.Lock()
	oldCloser := m.currentCloser
	m.currentPath = nextPath
	m.currentWriter = nextWriter
	m.currentCloser = nextCloser
	m.mu.Unlock()

	if oldCloser != nil {
		_ = oldCloser.Close()
	}
	return nil
}

func (m *loggingManager) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentWriter.Write(p)
}

func (m *loggingManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentCloser == nil {
		return nil
	}
	err := m.currentCloser.Close()
	m.currentCloser = nil
	return err
}

func defaultLogFilePath() string {
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "Library", "Logs", "AT-Term", "desktop.log")
		}
	case "windows":
		if base := os.Getenv("LocalAppData"); base != "" {
			return filepath.Join(base, "ATTerm", "Logs", "desktop.log")
		}
		if base, err := os.UserCacheDir(); err == nil && base != "" {
			return filepath.Join(base, "ATTerm", "Logs", "desktop.log")
		}
	default:
		if base := os.Getenv("XDG_STATE_HOME"); base != "" {
			return filepath.Join(base, "atterm", "desktop.log")
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state", "atterm", "desktop.log")
		}
	}
	return "desktop.log"
}

func isDevBuild(version string) bool {
	version = strings.TrimSpace(version)
	return version == "" || version == "dev"
}

func newDesktopLoggingManager(cfg appConfig, version string) (*loggingManager, error) {
	m, err := newLoggingManager(loggingOptions{
		devMode:  isDevBuild(version),
		terminal: os.Stderr,
	})
	if err != nil {
		return nil, err
	}
	if err := m.Apply(loggingConfigState{
		enabled: cfg.LogToFileEnabledOrDefault(),
		path:    cfg.LogFilePath,
	}); err != nil {
		_ = m.Close()
		return nil, err
	}
	return m, nil
}

func readLogPreview(path string, limit int64) (logPreview, error) {
	if limit <= 0 {
		limit = defaultLogPreviewBytes
	}
	preview := logPreview{Path: path}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return preview, nil
		}
		return preview, err
	}
	preview.Exists = true
	size := info.Size()
	start := int64(0)
	if size > limit {
		start = size - limit
		preview.Truncated = true
	}

	f, err := os.Open(path)
	if err != nil {
		return preview, err
	}
	defer f.Close()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return preview, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return preview, err
	}
	data = bytes.ToValidUTF8(data, []byte("?"))
	preview.Content = string(data)
	return preview, nil
}

type rotatingFileWriter struct {
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func newRotatingFileWriter(path string, maxBytes int64, maxBackups int) (*rotatingFileWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &rotatingFileWriter{
		path:       path,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
		file:       f,
		size:       info.Size(),
	}, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFileWriter) Close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *rotatingFileWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	if w.maxBackups > 0 {
		_ = os.Remove(w.rotatedPath(w.maxBackups))
		for i := w.maxBackups - 1; i >= 1; i-- {
			src := w.rotatedPath(i)
			dst := w.rotatedPath(i + 1)
			if _, err := os.Stat(src); err == nil {
				if err := os.Rename(src, dst); err != nil {
					return err
				}
			}
		}
		if err := os.Rename(w.path, w.rotatedPath(1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

func (w *rotatingFileWriter) rotatedPath(n int) string {
	return w.path + "." + strconv.Itoa(n)
}
