package intouristcore

import (
	"log"
	"strings"
	"sync"
)

// sinkWriter adapts LogSink.OnLog to io.Writer so the standard `log` package
// (used throughout the ported streams/upstream/wsapi code, unchanged from
// cmd/helper/main.go) reaches the Kotlin UI instead of an invisible stderr.
type sinkWriter struct {
	mu sync.Mutex
	s  LogSink
}

func (w *sinkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	s := w.s
	w.mu.Unlock()
	if s != nil {
		s.OnLog(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}

var logForwarderOnce sync.Once
var currentSinkWriter = &sinkWriter{}

// installLogForwarder points the standard `log` package at the given sink.
// Safe to call every time a mode starts; it just updates the target.
func installLogForwarder(s LogSink) {
	currentSinkWriter.mu.Lock()
	currentSinkWriter.s = s
	currentSinkWriter.mu.Unlock()
	logForwarderOnce.Do(func() {
		log.SetOutput(currentSinkWriter)
		log.SetFlags(0)
	})
}
