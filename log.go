package log

import (
	"time"
	"runtime"
	"os"
	"strings"
	"fmt"
	"sync"
	"path/filepath"
	"strconv"
	"io"
)

const (
	DEBUG = 1
	INFO  = 2
	WARN  = 3
	ERROR = 4
	FATAL = 5
)

const (
	kiloByte = 1024
	megaByte = kiloByte * 1024
	gigaByte = megaByte * 1024
)

type logger struct {
	level  int
	path   string
	format func(level int, line string, message string) string
	size   int64
	stdout bool
}

var (
	log logger = logger{
		level: DEBUG,
		path:  "./log",
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
		size:   megaByte,
		stdout: false}

	mutex = &sync.Mutex{}
)

func Path(path string) {
	log.path = path
}

func Level(level int) {
	if level > 0 && level < 6 {
		log.level = level
	}
}

func LevelAsString(level string) {
	switch strings.ToLower(level) {
	case "debug":
		Level(DEBUG)
	case "info":
		Level(INFO)
	case "warn":
		Level(WARN)
	case "error":
		Level(ERROR)
	case "fatal":
		Level(FATAL)
	default:
		Level(DEBUG)
	}
}

func Format(format func(level int, line string, message string) string) {
	log.format = format
}

func SizeLimit(size int64) {
	log.size = size
}

func Stdout(state bool) {
	log.stdout = state
}

func getFuncName() string {
	_, scriptName, line, _ := runtime.Caller(3)

	appPath, _ := os.Getwd()
	appPath += string(os.PathSeparator)

	return fmt.Sprintf("%s:%d", strings.Replace(scriptName, appPath, "", -1), line)
}

func getFilePath(appendLength int) (string, error) {
	timestamp := time.Now().Format("2006-01-02")
	path := log.path + string(os.PathSeparator) + timestamp + ".log"

	info, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return path, nil
	} else if info.Size()+int64(appendLength) <= log.size {
		return path, nil
	} else {
		increment, err := getMaxIncrement(path)
		if err != nil {
			return path, err
		}

		err = moveFile(path, fmt.Sprintf("%s.%d", path, increment+1))
		if err != nil {
			return path, err
		}
	}

	return path, nil
}

func getMaxIncrement(path string) (int, error) {
	matches, err := filepath.Glob(path + ".*")
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	if len(matches) > 0 {
		max := 0

		for _, match := range matches {
			match = strings.Replace(match, path, "", -1)
			i32, err := strconv.ParseInt(match, 10, 32)

			if err == nil {
				i := int(i32)

				if max < i {
					max = i
				}
			}
		}

		return max, nil
	}

	return 0, nil
}

func moveFile(sourceFilePath string, destinationFilePath string) (error) {
	source, err := os.Open(sourceFilePath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(destinationFilePath)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	err = os.Remove(sourceFilePath)
	if err != nil {
		return err
	}

	return nil
}

func write(level int, message string) {
	if log.level <= level {
		logLine := log.format(level, getFuncName(), message)
		filePath, err := getFilePath(len(logLine))
		if err != nil {
			fmt.Printf("Can't access to log file %s. Catch error %s\n", log.path, err.Error())

			return
		}

		mutex.Lock()

		file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)

		defer file.Sync()
		defer file.Close()
		defer mutex.Unlock()

		if err != nil {
			fmt.Printf("Can't write log to file %s. Catch error: %s\n", filePath, err.Error())

			return
		}

		_, err = file.WriteString(logLine + "\n")
		if err != nil {
			fmt.Printf("Can't write log to file %s. Catch error: %s\n", filePath, err.Error())

			return
		}

		if log.stdout {
			fmt.Println(logLine)
		}
	}
}

func Debug(message string) {
	write(DEBUG, message)
}

func Info(message string) {
	write(INFO, message)
}

func Warn(message string) {
	write(WARN, message)
}

func Error(message string) {
	write(ERROR, message)
}

func Fatal(message string) {
	write(FATAL, message)
}

func DebugFmt(message string, args ...interface{}) {
	write(DEBUG, fmt.Sprintf(message, args...))
}

func InfoFmt(message string, args ...interface{}) {
	write(INFO, fmt.Sprintf(message, args...))
}

func WarnFmt(message string, args ...interface{}) {
	write(WARN, fmt.Sprintf(message, args...))
}

func ErrorFmt(message string, args ...interface{}) {
	write(ERROR, fmt.Sprintf(message, args...))
}

func FatalFmt(message string, args ...interface{}) {
	write(FATAL, fmt.Sprintf(message, args...))
}
