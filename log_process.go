package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// 定义接口
type Reader interface {
	Read(rc chan<- []byte)
}
type Writer interface {
	Write(wc <-chan []byte)
}

type LogProcess struct {
	rc    chan []byte
	wc    chan []byte
	read  Reader
	write Writer
}

type ReadFromFile struct {
	path string // 日志文件的路径
}

func (r *ReadFromFile) Read(rc chan<- []byte) {
	f, err := os.Open(r.path)
	if err != nil {
		panic(fmt.Sprintf("open file error: %s", err.Error()))
	}
	f.Seek(0, 2) // 文件指针移动到末尾
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadBytes('\n')
		if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		rc <- line[:len(line)-1]
	}
}

type WriteToInfluxDB struct {
	influxDBDsn string // influxdb data source
}

func (w *WriteToInfluxDB) Write(wc <-chan []byte) {
	for v := range wc {
		fmt.Println(v)
	}
}

// 解析模块
func (l *LogProcess) Process() {
	for data := range l.rc {
		l.wc <- []byte(strings.ToUpper(string(data)))
	}
}

func main() {
	r := &ReadFromFile{
		path: "/tmp/access.log",
	}
	w := &WriteToInfluxDB{
		influxDBDsn: "username&password",
	}
	lp := &LogProcess{
		rc:    make(chan []byte),
		wc:    make(chan []byte),
		read:  r,
		write: w,
	}
	go lp.read.Read(lp.rc)
	go lp.Process()
	go lp.write.Write(lp.wc)

	// 此处主协程需要阻塞
	for {

	}
}
