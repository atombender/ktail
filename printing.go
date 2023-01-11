package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

func printInfo(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprint(os.Stderr, colorInfo("==> "+message+"\n"))
}

func printError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprint(os.Stderr, colorError("==> "+message+"\n"))
}

func formatTimestamp(t *time.Time) string {
	s := t.Local().Format("2006-01-02T15:04:05.999")
	for len(s) < 23 {
		s += "0"
	}
	return s
}

type kubeLogger struct{}

func (l *kubeLogger) Init(logr.RuntimeInfo)                  {}
func (l *kubeLogger) Enabled(int) bool                       { return true }
func (l *kubeLogger) WithValues(...interface{}) logr.LogSink { return l }
func (l *kubeLogger) WithName(string) logr.LogSink           { return l }

func (l *kubeLogger) Info(_ int, msg string, keysAndValues ...interface{}) {
	printInfo(formatKeysAndValues(msg, keysAndValues...))
}

func (l *kubeLogger) Error(_ error, msg string, keysAndValues ...interface{}) {
	printError(formatKeysAndValues(msg, keysAndValues...))
}

func formatKeysAndValues(msg string, kv ...interface{}) string {
	var sb strings.Builder
	_, _ = sb.WriteString(strings.TrimSpace(msg))
	for i := 0; i < len(kv)-1; i++ {
		_, _ = sb.WriteString(" ")
		_, _ = sb.WriteString(kv[i].(string))
		i++
		_, _ = sb.WriteString(fmt.Sprintf("=%v", kv[i]))
	}
	return strings.ReplaceAll(sb.String(), "%", "%%")
}
