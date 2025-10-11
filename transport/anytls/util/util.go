package util

import (
	"context"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/tiechui1994/tool/log"
)

func NewDeadlineWatcher(ddl time.Duration, timeOut func()) (done func()) {
	t := time.NewTimer(ddl)
	closeCh := make(chan struct{})
	go func() {
		defer t.Stop()
		select {
		case <-closeCh:
		case <-t.C:
			timeOut()
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(closeCh)
		})
	}
}

func StartRoutine(ctx context.Context, d time.Duration, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorln("[BUG] %v %s", r, string(debug.Stack()))
			}
		}()
		for {
			time.Sleep(d)
			f()
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
}

type StringMap map[string]string

func (s StringMap) ToBytes() []byte {
	var lines []string
	for k, v := range s {
		lines = append(lines, k+"="+v)
	}
	return []byte(strings.Join(lines, "\n"))
}

func StringMapFromBytes(b []byte) StringMap {
	var m = make(StringMap)
	var lines = strings.Split(string(b), "\n")
	for _, line := range lines {
		v := strings.SplitN(line, "=", 2)
		if len(v) == 2 {
			m[v[0]] = v[1]
		}
	}
	return m
}

type DialOutFunc func(ctx context.Context) (net.Conn, error)
