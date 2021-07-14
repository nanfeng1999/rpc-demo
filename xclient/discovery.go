/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/11 11:24
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package xclient

import (
	"errors"
	"math/rand"
	"net/http"
	"rpc/logger"
	"strings"
	"sync"
	"time"
)

type SelectMode int

// 定义两种选择模式
const (
	RandomSelect SelectMode = iota
	RoundRobinSelect
)

// 定义发现服务接口

type Discovery interface {
	Refresh() error                      // 自动更新服务列表
	Update([]string) error               // 手动更新服务列表
	Get(mode SelectMode) (string, error) // 传入string获取服务
	GetAll() ([]string, error)           // 获取服务列表
}

// 接着我们实现一个手工维护的服务发现结构体

type MultiServerDiscovery struct {
	r       *rand.Rand   // 用于随机生成随机数
	servers []string     // 服务列表
	mu      sync.RWMutex // 锁
	index   int          //当前到的索引值
}

func (d *MultiServerDiscovery) Refresh() error {
	return nil
}

func (d *MultiServerDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

func (d *MultiServerDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}
	switch mode {
	case RandomSelect:
		server := d.servers[d.r.Intn(n)]
		return server, nil
	case RoundRobinSelect:
		server := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return server, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *MultiServerDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// return a copy of d.servers
	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}

var _ Discovery = (*MultiServerDiscovery)(nil)

func NewMultiServerDiscovery(servers []string) *MultiServerDiscovery {
	return &MultiServerDiscovery{
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
		servers: servers,
		index:   rand.Int(),
	}
}

type RegistryDiscovery struct {
	*MultiServerDiscovery               // 继承
	registry              string        // 注册中心地址
	timeOut               time.Duration // 超时时间
	lastUpdate            time.Time     // 最后从服务中心更新列表的时间 默认10s
}

const defaultUpdateTimeout = time.Second * 10

func NewRegistryDiscovery(registry string, timeOut time.Duration) *RegistryDiscovery {
	if timeOut == 0 {
		timeOut = defaultUpdateTimeout
	}
	return &RegistryDiscovery{
		MultiServerDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:             registry,
		timeOut:              timeOut, //超时时间
	}
}

func (r *RegistryDiscovery) Update(servers []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers = servers
	r.lastUpdate = time.Now()
	return nil
}

func (r *RegistryDiscovery) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastUpdate.Add(r.timeOut).After(time.Now()) {
		return nil
	}
	logger.Logger.Println("rpc registry: refresh servers from registry", r.registry)
	// 只有过期了才需要请求注册中心 刷新服务器
	resp, err := http.Get(r.registry)
	if err != nil {
		logger.Logger.Println("refresh servers fail")
		return err
	}
	servers := strings.Split(resp.Header.Get("X-RPC-Servers"), ",")
	r.servers = make([]string, 0)
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			r.servers = append(r.servers, strings.TrimSpace(server))
		}
	}
	r.lastUpdate = time.Now()
	return nil
}

func (r *RegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := r.Refresh(); err != nil {
		return "", err
	}
	return r.MultiServerDiscovery.Get(mode)
}

func (r *RegistryDiscovery) GetAll() ([]string, error) {
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	return r.MultiServerDiscovery.GetAll()
}
