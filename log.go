package log

import (
	"fmt"
	"log/syslog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DEBUG = iota
	INFO
	WARN
	ERROR
	FATAL
)

const (
	kiloByte = 1024
	megaByte = kiloByte * 1024
	gigaByte = megaByte * 1024
)

type config struct {
	level        int
	path         string
	saveInFile   bool
	syslog       *syslog.Writer
	saveInSyslog bool
	ttl          int64
	deleteOld    bool
	format       func(level int, line, message string) string
	extension    string
	size         int64
	logChan      chan log
	stdout       bool
	once         *sync.Once
	wg           *sync.WaitGroup
	mx           *sync.RWMutex
	notifier     notifier
}
type notifier struct {
	callback func(level int, message string)
	level    int
}

type log struct {
	level   int
	message string
}

var cfg = config{
	level: DEBUG,
	format: func(level int, line string, message string) string {
		now := time.Now().Format("2006-01-02 15:04:05")
		levelStr := "DEBUG"

		switch level {
		case DEBUG:
			levelStr = "DEBUG"
		case INFO:
			levelStr = "INFO"
		case WARN:
			levelStr = "WARN"
		case ERROR:
			levelStr = "ERROR"
		case FATAL:
			levelStr = "FATAL"
		}

		data := []string{
			now,
			levelStr,
			line,
			message}

		return strings.Join(data, "\t")
	},
	size:      -1,
	logChan:   make(chan log, 100),
	stdout:    false,
	extension: "log",
	once:      &sync.Once{},
	wg:        &sync.WaitGroup{},
	mx:        &sync.RWMutex{},
	notifier: notifier{
		callback: func(level int, message string) {},
		level:    DEBUG}}

func Path(path string) (err error) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.path = path
	cfg.saveInFile = true

	err = os.MkdirAll(cfg.path, 0755)

	return
}

func Syslog(tag string) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	var err error
	cfg.syslog, err = syslog.New(syslog.LOG_DEBUG|syslog.LOG_USER, tag)
	if err != nil {
		fmt.Printf("Can't init syslog with tag %s. Catch error %s\n", tag, err.Error())

		return
	}

	cfg.saveInSyslog = true
}

func Level(level int) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	if level >= 0 && level < 5 {
		cfg.level = level
	}
}

func LevelAsString(level string) {
	Level(getLevelFromString(level))
}

func Format(format func(level int, line string, message string) string) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.format = format
}

func SizeLimit(size int64) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.size = size
}

func Stdout(state bool) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.stdout = state
}

func TTL(ttl int64) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.ttl = ttl
	cfg.deleteOld = true
}

func Extension(extension string) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.extension = extension
}

func getLevelFromString(level string) int {
	switch strings.ToLower(level) {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn":
		return WARN
	case "error":
		return ERROR
	case "fatal":
		return FATAL
	default:
		return DEBUG
	}
}

func getFuncName() string {
	_, scriptName, line, _ := runtime.Caller(3)

	appPath, _ := os.Getwd()
	appPath += string(os.PathSeparator)

	return fmt.Sprintf("%s:%d", strings.Replace(scriptName, appPath, "", -1), line)
}

func getFilePath(appendLength int) (path string, err error) {
	cfg.mx.RLock()
	defer cfg.mx.RUnlock()

	timestamp := time.Now().Format("2006-01-02")
	path = cfg.path + string(os.PathSeparator) + timestamp + "." + cfg.extension

	path, err = filepath.Abs(path)
	if err != nil {
		return
	}

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return path, nil
	} else if cfg.size < 0 ||
		info.Size()+int64(appendLength) <= cfg.size {
		return path, nil
	} else {
		var increment int
		increment, err = getMaxIncrement(path)
		if err != nil {
			return
		}

		err = moveFile(path, fmt.Sprintf("%s.%d", path, increment+1))
		if err != nil {
			return
		}
	}

	return
}

func getMaxIncrement(path string) (incr int, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return
	}

	matches, err := filepath.Glob(path + ".*")
	if os.IsNotExist(err) {
		return
	} else if err != nil {
		return
	}

	if len(matches) > 0 {
		for _, match := range matches {
			match = strings.Replace(match, path+".", "", -1)
			var i64 int64
			i64, err = strconv.ParseInt(match, 10, 32)

			if err == nil {
				i := int(i64)

				if incr < i {
					incr = i
				}
			} else {
				return
			}
		}

		return
	}

	return
}

func moveFile(sourceFilePath string, destinationFilePath string) error {
	return os.Rename(sourceFilePath, destinationFilePath)
}

func handle(l log) {
	cfg.mx.RLock()
	defer cfg.mx.RUnlock()

	if cfg.level <= l.level {
		cfg.wg.Add(1)
		l.message = cfg.format(l.level, getFuncName(), l.message)
		cfg.logChan <- l
	}

	cfg.once.Do(func() {
		if cfg.deleteOld {
			go watchOld()
		}

		go func(logChan chan log) {
			for log := range logChan {
				if cfg.notifier.level <= log.level {
					cfg.notifier.callback(log.level, log.message)
				}

				printToStdout(log)
				writeToFile(log)
				writeToSyslog(log)

				cfg.wg.Done()
			}
		}(cfg.logChan)
	})
}

func printToStdout(l log) {
	if cfg.stdout {
		var err error

		if l.level < WARN {
			_, err = fmt.Fprintln(os.Stdout, l.message)
			if err != nil {
				fmt.Printf("Can't write to stdout. Catch error %s\n", err.Error())
			}
		} else {
			_, err = fmt.Fprintln(os.Stderr, l.message)
			if err != nil {
				fmt.Printf("Can't write to stderr. Catch error %s\n", err.Error())
			}
		}
	}
}

func writeToFile(l log) {
	if cfg.saveInFile {
		filePath, err := getFilePath(len(l.message))
		if err != nil {
			fmt.Printf("Can't access to log file %s. Catch error %s\n", cfg.path, err.Error())

			return
		}

		file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			fmt.Printf("Can't write log to file %s. Catch error: %s\n", filePath, err.Error())

			return
		}

		defer func() {
			err = file.Sync()
			if err != nil {
				fmt.Printf("Can't sync log file %s. Catch error: %s\n", filePath, err.Error())
			}

			err = file.Close()
			if err != nil {
				fmt.Printf("Can't sync log file %s. Catch error: %s\n", filePath, err.Error())
			}
		}()

		_, err = file.WriteString(l.message + "\n")
		if err != nil {
			fmt.Printf("Can't write log to file %s. Catch error: %s\n", filePath, err.Error())
		}
	}
}

func writeToSyslog(l log) {
	var err error

	if cfg.saveInSyslog {
		switch l.level {
		case FATAL:
			err = cfg.syslog.Emerg(l.message)
		case ERROR:
			err = cfg.syslog.Err(l.message)
		case WARN:
			err = cfg.syslog.Warning(l.message)
		case INFO:
			err = cfg.syslog.Info(l.message)
		case DEBUG:
			err = cfg.syslog.Debug(l.message)
		}
	}

	if err != nil {
		fmt.Printf("Can't write log to syslog. Catch error: %s\n", err.Error())
	}
}

func watchOld() {
	for {
		deleteOld()

		time.Sleep(time.Hour)
	}
}

func deleteOld() {
	paths, err := filepath.Glob(cfg.path + string(filepath.Separator) + "*")
	if err != nil {
		fmt.Printf("Can't access to log file %s. Catch error %s\n", cfg.path, err.Error())

		return
	} else {
		ttl := float64(cfg.ttl)

		for _, path := range paths {
			file, err := os.Stat(path)
			if err != nil {
				fmt.Printf("Can't access to log file %s. Catch error %s\n", cfg.path, err.Error())

				return
			} else if !file.IsDir() {
				if time.Now().Sub(file.ModTime()).Seconds() > ttl {
					err = os.Remove(path)
					if err != nil {
						fmt.Printf("Can't remove old log file %s. Catch error %s\n", path, err.Error())

						return
					}
				}
			}
		}
	}
}

func Flush() {
	cfg.wg.Wait()
}

func Notifier(callback func(level int, message string), level string) {
	cfg.mx.Lock()
	defer cfg.mx.Unlock()

	cfg.notifier = notifier{
		callback: callback,
		level:    getLevelFromString(level)}
}

func Debug(message string) {
	handle(log{level: DEBUG, message: message})
}

func Info(message string) {
	handle(log{level: INFO, message: message})
}

func Warn(message string) {
	handle(log{level: WARN, message: message})
}

func Error(message string) {
	handle(log{level: ERROR, message: message})
}

func Fatal(message string) {
	handle(log{level: FATAL, message: message})
}

func DebugFmt(message string, args ...interface{}) {
	handle(log{level: DEBUG, message: fmt.Sprintf(message, args...)})
}

func InfoFmt(message string, args ...interface{}) {
	handle(log{level: INFO, message: fmt.Sprintf(message, args...)})
}

func WarnFmt(message string, args ...interface{}) {
	handle(log{level: WARN, message: fmt.Sprintf(message, args...)})
}

func ErrorFmt(message string, args ...interface{}) {
	handle(log{level: ERROR, message: fmt.Sprintf(message, args...)})
}

func FatalFmt(message string, args ...interface{}) {
	handle(log{level: FATAL, message: fmt.Sprintf(message, args...)})
}
