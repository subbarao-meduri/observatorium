package remotewrite

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

const (
	successWrite = "Metrics forwarded successfully"
)

var (
	logMaxCount = int64(10)
	logInterval = 600 * time.Second

	rlogger     log.Logger
	LogChannels = []chan logMessage{}
)

type logMessage struct {
	messageKey string
	keyvals    []interface{}
}

type logCounter struct {
	logKey        string
	LogTimestamps []time.Time
	logStartTime  *time.Time
	logNumber     int64
	reducedLog    bool
}

func revertCounter(counter *logCounter) {
	if counter.reducedLog {
		counter.reducedLog = false
		counter.LogTimestamps = []time.Time{}
	}
}

func InitChannels(logger log.Logger, size int) {
	rlogger = logger
	if os.Getenv("LOG_MAX_COUNT") != "" {
		v, err := strconv.ParseInt(os.Getenv("LOG_MAX_COUNT"), 10, 0)
		if err != nil {
			logMaxCount = v
		}
	}
	if os.Getenv("LOG_INTERVAL") != "" {
		v, err := time.ParseDuration(os.Getenv("LOG_INTERVAL"))
		if err != nil {
			logInterval = v
		}
	}
	for i := 0; i < size; i++ {
		LogChannels = append(LogChannels, make(chan logMessage))
	}
	for i := 0; i < size; i++ {
		j := i
		counter := &logCounter{
			LogTimestamps: []time.Time{},
		}
		go func() {
			for {
				select {
				case message := <-LogChannels[j]:
					if message.messageKey == successWrite {
						revertCounter(counter)
					} else {
						checkLog(counter, message.messageKey, message.keyvals...)
					}
				case <-time.After(logInterval):
					revertCounter(counter)
				}
			}
		}()
	}
}

func checkLog(counter *logCounter, key string, keyvals ...interface{}) {
	if key != counter.logKey {
		counter.logKey = key
		counter.LogTimestamps = []time.Time{time.Now()}
		counter.reducedLog = false
		level.Error(rlogger).Log(keyvals...)
	}
	if counter.reducedLog {
		if time.Since(*counter.logStartTime) >= logInterval {
			message := fmt.Sprintf("Error occurred %d times in last %d seconds: %s",
				counter.logNumber, int(time.Since(*counter.logStartTime).Seconds()), key)
			keyvals[1] = message
			level.Error(rlogger).Log(keyvals...)
			if counter.logNumber < logMaxCount {
				counter.reducedLog = false
				counter.LogTimestamps = []time.Time{}
			} else {
				counter.logNumber = 0
				now := time.Now()
				counter.logStartTime = &now
			}
		} else {
			counter.logNumber = counter.logNumber + 1
		}
	} else {
		if int64(len(counter.LogTimestamps)) == logMaxCount {
			if time.Since(counter.LogTimestamps[0]) <= logInterval {
				counter.reducedLog = true
				counter.LogTimestamps = []time.Time{}
				counter.logNumber = 1
				now := time.Now()
				counter.logStartTime = &now
			} else {
				counter.LogTimestamps[0] = time.Time{}
				counter.LogTimestamps = counter.LogTimestamps[1:]
				counter.LogTimestamps = append(counter.LogTimestamps, time.Now())
				level.Error(rlogger).Log(keyvals...)
			}
		} else {
			counter.LogTimestamps = append(counter.LogTimestamps, time.Now())
			level.Error(rlogger).Log(keyvals...)
		}
	}
}
