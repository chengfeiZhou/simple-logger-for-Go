package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/influxdb/client/v2"
)

const (
	TypeHandleLine = 0
	TypeErrNum     = 1
)

var TypeMonitorchan = make(chan int, 200)

// 系统监控数据
type SystemInfo struct {
	HandleLine      int     `json:"handleLine"`      // 总处理日志行数
	TPS             float64 `json:"tps"`             //系统吞吐量
	ReadChannelLen  int     `json:"readChannelLen"`  // read channel长度
	WriteChannelLen int     `json:"writeChannelLen"` // write channel长度
	RunTime         string  `json:"runTime"`         // 运行总时间
	ErrNum          int     `json:"errNum"`          // 错误数
}

type Monitor struct {
	startTime time.Time
	data      SystemInfo
	tpsSil    []int
}

func (m *Monitor) start(lp *LogProcess) {
	go func() {
		for n := range TypeMonitorchan {
			switch n {
			case TypeErrNum:
				m.data.ErrNum += 1
			case TypeHandleLine:
				m.data.HandleLine += 1
			}
		}
	}()
	ticker := time.NewTicker(time.Second * 5)
	go func() {
		for {
			<-ticker.C
			m.tpsSil = append(m.tpsSil, m.data.HandleLine)
			if len(m.tpsSil) > 2 {
				m.tpsSil = m.tpsSil[1:]
			}
		}
	}()
	http.HandleFunc("/monitor", func(w http.ResponseWriter, r *http.Request) {
		m.data.RunTime = time.Now().Sub(m.startTime).String()
		m.data.ReadChannelLen = len(lp.rc)
		m.data.WriteChannelLen = len(lp.wc)
		if len(m.tpsSil) > 1 {
			m.data.TPS = float64((m.tpsSil[1] - m.tpsSil[0]) / 5)
		}
		ret, _ := json.MarshalIndent(m.data, "", "\t")
		io.WriteString(w, string(ret))
	})
	http.ListenAndServe(":9093", nil)
}

// 定义接口
type Reader interface {
	Read(rc chan<- []byte)
}
type Writer interface {
	Write(wc <-chan *Message)
}

type LogProcess struct {
	rc    chan []byte
	wc    chan *Message
	read  Reader
	write Writer
}

type ReadFromFile struct {
	path string // 日志文件的路径
}

func (r *ReadFromFile) Read(rc chan<- []byte) {
	f, err := os.Open(r.path)
	if err != nil {
		TypeMonitorchan <- TypeErrNum
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
		TypeMonitorchan <- TypeHandleLine
		rc <- line[:len(line)-1]
	}
}

type WriteToInfluxDB struct {
	influxDBDsn string // influxdb data source
}

func (w *WriteToInfluxDB) Write(wc <-chan *Message) {
	infSli := strings.Split(w.influxDBDsn, "@")
	fmt.Println(infSli)
	// Create a new HTTPClient
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     infSli[0],
		Username: infSli[1],
		Password: infSli[2],
	})
	if err != nil {
		TypeMonitorchan <- TypeErrNum
		log.Fatal(err)
	}
	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  infSli[3],
		Precision: infSli[4],
	})
	if err != nil {
		TypeMonitorchan <- TypeErrNum
		log.Fatal(err)
	}

	for v := range wc {
		// Create a point and add to batch
		// Tag: path, methods, Scheme, status
		tags := map[string]string{"path": v.Path, "methods": v.Methods, "scheme": v.Scheme, "status": v.Status}
		// Fields: upstream, requestTime, bytesSent
		fields := map[string]interface{}{
			"upstream":    v.UpstreamTime,
			"requestTime": v.RequestTime,
			"bytesSent":   v.BytesSent,
		}

		pt, err := client.NewPoint("nginx_log", tags, fields, v.TimeLocal)
		if err != nil {
			TypeMonitorchan <- TypeErrNum
			log.Fatal(err)
			continue
		}
		bp.AddPoint(pt)

		// Write the batch
		if err := c.Write(bp); err != nil {
			TypeMonitorchan <- TypeErrNum
			log.Fatal(err)
			continue
		}

		// Close client resources
		if err := c.Close(); err != nil {
			TypeMonitorchan <- TypeErrNum
			log.Fatal(err)
			continue
		}
	}
}

// 消息类型
type Message struct {
	TimeLocal                     time.Time
	BytesSent                     int
	Path, Methods, Scheme, Status string
	UpstreamTime, RequestTime     float64
}

// 解析模块
func (l *LogProcess) Process() {
	/**
	192.168.1.1 - - [04/Mar/2021:13:49:52 +0000] http "GET /foo?query=t HTTP/1.0" 200 2133 "-" "KeepAliveClient" "-" 1.005 1.854
	*/
	r := regexp.MustCompile(`([\d\.]+)\s+([^ \[]+)\s+([^ \[]+)\s+\[([^\]]+)\]\s+([a-z]+)\s+\"([^"]+)\"\s+(\d{3})\s+(\d+)\s+\"([^"]+)\"\s+\"(.*?)\"\s+\"([\d\.-]+)\"\s+([\d\.-]+)\s+([\d\.-]+)`)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for data := range l.rc {
		ret := r.FindStringSubmatch(string(data))
		if len(ret) != 14 {
			log.Panicln("匹配失败: ", string(data))
		}

		t, err := time.ParseInLocation("02/Jan/2006:15:04:05 +0000", ret[4], loc)
		if err != nil {
			TypeMonitorchan <- TypeErrNum
			log.Panicln("时间解析失败: ", err)
		}
		bytesSent, _ := strconv.Atoi(ret[8])

		// GET /foo?query=t HTTP/1.0
		reqSli := strings.Split(ret[6], " ")
		if len(reqSli) != 3 {
			log.Panicln("请求解析失败: ", reqSli)
		}
		u, _ := url.Parse(reqSli[1])
		upstreamTime, _ := strconv.ParseFloat(ret[12], 64)
		requestTime, _ := strconv.ParseFloat(ret[13], 64)
		msg := &Message{
			TimeLocal:    t,
			BytesSent:    bytesSent,
			Methods:      reqSli[0],
			Path:         u.Path,
			Scheme:       ret[5],
			Status:       ret[7],
			UpstreamTime: upstreamTime,
			RequestTime:  requestTime,
		}
		l.wc <- msg
	}
}

func main() {
	var path, influDsn string
	flag.StringVar(&path, "path", "./tmp/access.log", "log file path")
	flag.StringVar(&influDsn, "influDsn", "http://127.0.0.1:8086@admin@admin123@mydb@s", "influDB data source")
	flag.Parse()

	r := &ReadFromFile{
		path: path,
	}
	w := &WriteToInfluxDB{
		influxDBDsn: influDsn,
	}
	lp := &LogProcess{
		rc:    make(chan []byte, 200),
		wc:    make(chan *Message, 200),
		read:  r,
		write: w,
	}
	go lp.read.Read(lp.rc)
	for i := 0; i < 2; i++ {
		// 解析速度较慢
		go lp.Process()
	}
	for i := 0; i < 4; i++ {
		// 写入速度最慢慢
		go lp.write.Write(lp.wc)
	}
	// 此处主协程需要阻塞
	m := Monitor{
		startTime: time.Now(),
		data:      SystemInfo{},
	}
	m.start(lp)
}
