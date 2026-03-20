package log

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Init configures the global logrus logger.
func Init(debug bool, logPath string) {
	if logPath == "-" || logPath == "" {
		logrus.SetOutput(os.Stdout)
	} else {
		if w := fileWriter(logPath); w != nil {
			logrus.SetOutput(w)
		}
	}

	hostname, _ := os.Hostname()
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		FieldMap: logrus.FieldMap{
			"hostname": hostname,
		},
	})

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}
}

func fileWriter(path string) io.Writer {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil && !os.IsExist(err) {
		logrus.Warnf("failed to create log directory %s: %v", dir, err)
		return nil
	}

	return &lumberjack.Logger{
		Filename: path,
		MaxSize:  200, // megabytes
		MaxAge:   7,   // days
		Compress: true,
	}
}
