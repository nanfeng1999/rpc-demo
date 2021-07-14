/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/11 11:24
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package xclient

import (
	"context"
	"reflect"
	"rpc/client"
	"rpc/option"
	"sync"
)

type XClient struct {
	d       Discovery                 // 服务
	mode    SelectMode                // 选择的模式
	clients map[string]*client.Client // 客户端集合
	mu      sync.RWMutex              // 读写锁
	opt     *option.Option            // 选项
}

func NewXClient(d Discovery, mode SelectMode, opt *option.Option) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*client.Client),
	}
}

func (xclient *XClient) Close() error {
	xclient.mu.Lock()
	defer xclient.mu.Unlock()
	for name, client := range xclient.clients {
		_ = client.Close()
		delete(xclient.clients, name)
	}
	return nil
}

// 传入rpc地址发起一个会话
func (xclent *XClient) dial(rpcAddr string) (*client.Client, error) {
	// 这里要整个过程全部锁住 不然for循环加广播 会同时创建10个连接 其实用到的只有两个连接
	xclent.mu.Lock()
	defer xclent.mu.Unlock()
	// 延迟初始化
	//if xclent.clients == nil {
	//	xclent.clients = make(map[string]*client.Client)
	//}
	// 如果这个地址指定的会话不存在的话
	cli, ok := xclent.clients[rpcAddr]
	// 如果能取出 判断是否有效
	// 如果有因为错误关闭的话 清除然后重新创建一个新的连接 FIXME
	if ok && !cli.IsValid() {
		_ = cli.Close()
		cli = nil
		delete(xclent.clients, rpcAddr)
	}
	// 调用xDial重新建立一次连接 并存入map中去
	if cli == nil {
		var err error
		cli, err = client.XDial(rpcAddr, xclent.opt)
		if err != nil {
			return nil, err
		}
		xclent.clients[rpcAddr] = cli
	}
	// 返回刚才建立的client
	return cli, nil
}

func (xclent *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	cli, err := xclent.dial(rpcAddr)
	if err != nil {
		return err
	}
	return cli.Call(ctx, serviceMethod, args, reply)
}

// Call 最后再加入一层选择模式
func (xclient *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xclient.d.Get(xclient.mode)
	if err != nil {
		return err
	}
	return xclient.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// BroadCast 广播函数
func (xclient *XClient) BroadCast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddrs, err := xclient.d.GetAll()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var e error
	var replyDone = false
	wg := new(sync.WaitGroup)
	mu := new(sync.Mutex)
	for _, rpcAddr := range rpcAddrs {
		wg.Add(1)
		go func(rpcAddr string) {
			copyReply := reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			err := xclient.call(rpcAddr, ctx, serviceMethod, args, copyReply)
			mu.Lock()
			if err != nil && e == nil {
				//logger.Logger.Println("addr ", rpcAddr, "error:", err)
				e = err
				cancel()
			}
			if err == nil && !replyDone {
				replyDone = true
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(copyReply).Elem())
			}
			mu.Unlock()
			defer wg.Done()
		}(rpcAddr)
	}
	wg.Wait()
	cancel()
	return e
}

//func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
//	servers, err := xc.d.GetAll()
//	if err != nil {
//		return err
//	}
//	var wg sync.WaitGroup
//	var mu sync.Mutex // protect e and replyDone
//	var e error
//	replyDone := reply == nil // if reply is nil, don't need to set value
//	ctx, cancel := context.WithCancel(ctx)
//	for _, rpcAddr := range servers {
//		wg.Add(1)
//		go func(rpcAddr string) {
//			defer wg.Done()
//			var clonedReply interface{}
//			if reply != nil {
//				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
//			}
//			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
//			mu.Lock()
//			if err != nil && e == nil {
//				e = err
//				cancel() // if any call failed, cancel unfinished calls
//			}
//			if err == nil && !replyDone {
//				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
//				replyDone = true
//			}
//			mu.Unlock()
//		}(rpcAddr)
//	}
//	wg.Wait()
//	return e
//}
