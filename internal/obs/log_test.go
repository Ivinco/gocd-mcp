package obs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLogger_StderrWhenNoFile(t *testing.T) {
	log, closer, err := NewLogger("info", "")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer closer.Close()
	if log == nil {
		t.Fatal("nil logger")
	}
}

func TestNewLogger_WritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	log, closer, err := NewLogger("info", path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	log.Info("hello", "k", "v")
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), `"msg":"hello"`) || !strings.Contains(string(data), `"k":"v"`) {
		t.Fatalf("log line not in file: %s", data)
	}
}
