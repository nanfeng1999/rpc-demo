/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/14 16:12
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package registry

import (
	"log"
	"net/http"
	"rpc/logger"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeOut = 5 * time.Second
	defaultPath    = "/_rpc_/registry" // 注册默认地址
)

type Registry struct {
	timeOut     time.Duration          // 超时时间
	mu          sync.Mutex             // 互斥锁
	serverItems map[string]*ServerItem // 注册中心服务器集合
}

type ServerItem struct {
	Address string    // 地址
	start   time.Time // 服务注册时间
}

func New(timeOut time.Duration) *Registry {
	return &Registry{
		timeOut:     timeOut,
		serverItems: make(map[string]*ServerItem),
	}
}

var DefaultRegistry = New(defaultTimeOut)

func (r *Registry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item, ok := r.serverItems[addr]; ok {
		item.start = time.Now()
	} else {
		r.serverItems[addr] = &ServerItem{Address: addr, start: time.Now()}
	}
}

func (r *Registry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	for _, item := range r.serverItems {
		// 如果超时时间 开始时间加上超时时间是在现在的时间后面的话 说明没有超时 现在的时间还是超市时间的范围内 或者没有设置超时时间的话 那么就判定都是alive
		if item.start.Add(r.timeOut).After(time.Now()) || r.timeOut == 0 {
			alive = append(alive, item.Address)
		} else {
			// 如果时间超时的话 删除对应的项目
			delete(r.serverItems, item.Address)
		}
	}
	// 增序排列
	sort.Strings(alive)
	return alive
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	// Get请求获取当前所有的服务地址
	case "GET":
		w.Header().Set("X-RPC-Servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		addr := req.Header.Get("X-RPC-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Logger.Println("have no addr")
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultPath)
}

func HeartBeat(register, address string, duration time.Duration) {
	// 默认周期比超时周期少1分钟
	if duration == 0 {
		duration = defaultTimeOut - time.Duration(1)*time.Minute
	}
	// 心跳检测  设置一个定时器 每duration时间发送一次心跳检测包
	var err error
	err = sendHeatBeat(register, address)
	go func() {
		ticker := time.NewTimer(duration)
		defer ticker.Stop()
		for err == nil {
			<-ticker.C
			err = sendHeatBeat(register, address)
		}
	}()
}

func sendHeatBeat(registry, address string) error {
	logger.Logger.Println(address, "send heart beat to registry", registry)
	client := http.Client{}
	req, err := http.NewRequest("POST", registry, nil)
	if err != nil {
		logger.Logger.Println("create new request fail,err:", err)
		return err
	}
	req.Header.Set("X-RPC-Server", address)
	if _, err = client.Do(req); err != nil {
		logger.Logger.Println("get response fail,err:", err)
		return err
	}
	return nil
}
