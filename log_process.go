package main

import (
	"fmt"
	"strings"
)

// 定义接口
type Reader interface {
	Read(rc chan<- string)
}
type Writer interface {
	Write(wc <-chan string)
}

type LogProcess struct {
	rc    chan string
	wc    chan string
	read  Reader
	write Writer
}

type ReadFromFile struct {
	path string // 日志文件的路径
}

func (r *ReadFromFile) Read(rc chan<- string) {
	line := "message"
	rc <- line
}

type WriteToInfluxDB struct {
	influxDBDsn string // influxdb data source
}

func (w *WriteToInfluxDB) Write(wc <-chan string) {
	dd := <-wc
	fmt.Println(dd)
}

// 解析模块
func (l *LogProcess) Process() {
	data := <-l.rc
	l.wc <- strings.ToUpper(data)
}

func main() {
	r := &ReadFromFile{
		path: "/tmp/access.log",
	}
	w := &WriteToInfluxDB{
		influxDBDsn: "username&password",
	}
	lp := &LogProcess{
		rc:    make(chan string),
		wc:    make(chan string),
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
