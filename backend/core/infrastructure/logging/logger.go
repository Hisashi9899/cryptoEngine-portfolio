package logging

import (
	"io"
	"log"
	"os"
	"time"
)

type Logger struct {
	info  *log.Logger
	warn  *log.Logger
	err   *log.Logger
	debug *log.Logger
}

func NewLogger() *Logger {
	return NewLoggerTo(os.Stdout)
}

// NewLoggerTo は指定した writer に出力する logger を返す。
// バックテストのように JSON 出力とログを混ぜたくない場合に io.Discard などを渡す。
func NewLoggerTo(out io.Writer) *Logger {
	flags := log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
	return &Logger{
		info:  log.New(out, "[INFO]  ", flags),
		warn:  log.New(out, "[WARN]  ", flags),
		err:   log.New(os.Stderr, "[ERROR] ", flags),
		debug: log.New(out, "[DEBUG] ", flags),
	}
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.info.Printf(format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.warn.Printf(format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.err.Printf(format, args...)
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.debug.Printf(format, args...)
}

func (l *Logger) InfoWithElapsed(start time.Time, format string, args ...interface{}) {
	elapsed := time.Since(start)
	l.info.Printf("[%v] "+format, append([]interface{}{elapsed}, args...)...)
}
