/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/1 20:15
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"rpc/codec"
	"rpc/logger"
	"rpc/option"
	"strings"
	"sync"
	"time"
)

const (
	connected        = "200 Connected to Gee RPC"
	defaultRPCPath   = "/_rpc_"
	defaultDebugPath = "/debug/rpc"
)

// Client 首先需要一个Client结构体 用来发送请求和接收回复
type Client struct {
	Codec    codec.Codec      // 编解码器
	Seq      uint64           // 当前发送命令的序列号
	Pending  map[uint64]*Call // 没有处理完毕的Call请求
	Opt      *option.Option   // 编码方式
	mu       *sync.Mutex      // 锁 用来保护map
	header   codec.Header     // 为什么header不写在Client中？因为header是用来通信的语义，而call存诸的是本地调用的传入参数和返回结果这些 同时这样可以减少代码的重用性
	sending  *sync.Mutex      // 发消息锁
	Closing  bool             // 用户主动关闭
	ShutDown bool             // 处理出现错误关闭
}

func (c *Client) IsValid() bool {
	if c.ShutDown == false && c.Closing == false {
		return true
	}
	return false
}

var ErrShutdown = errors.New("the conn is closed")

func (c *Client) Close() error {
	if c.ShutDown == true || c.Closing == true {
		return ErrShutdown
	}
	c.Closing = true
	return c.Codec.Close()
}

// 客户端主要有两个功能 一个是发送请求 一个是接收回复
func (c *Client) send(call *Call) {
	// 确保发送包不会混淆在一起
	c.sending.Lock()
	defer c.sending.Unlock()
	seq, err := c.registerCall(call)
	if err != nil {
		call.Err = err
		call.Done()
		return
	}
	// FIXME 为什么不在call中放入header？
	c.header.Seq = seq
	c.header.ServiceMethod = call.ServiceMethod
	c.header.Err = ""
	// 如果写失败的话 call为什么会变nil？ 假如header写入成功并被读取成功 那么call会被删除 但是
	// 参数写入失败的话这里报错err 这个时候call已经被删除了
	// 但是为什么不把call写在后面呢？ 前面只写一个getCall函数 在读取了body之后再删除
	err = c.Codec.Write(&c.header, call.Args)

	if err != nil {
		call = c.removeCall(seq)
		if call != nil {
			call.Err = err
			call.Done()
		}
	}
}

func (c *Client) receive() {
	var err error
	for err == nil {
		var header codec.Header
		if err = c.Codec.ReadHeader(&header); err != nil {
			break
		}
		// 删除是表示已经处理完毕的call调用
		call := c.removeCall(header.Seq)
		switch {
		case call == nil:
			// 原因是因为调用已经被删除了
			logger.Logger.Println("call is empty")
			// FIXME 即使错误也要把后面的数据读出来 为什么？
			err = c.Codec.ReadBody(nil)
		case c.header.Err != "":
			// FIXME 我为啥加了这一行 傻逼了？
			//call.Reply = errors.New(header.Err)
			// 即使错误也要把后面的数据读出来 为什么？
			call.Err = fmt.Errorf(header.Err)
			err = c.Codec.ReadBody(nil)
			// pending中已经删除了 这里还Done有什么意义？
			call.Done()
		default:
			err = c.Codec.ReadBody(call.Reply)
			if err != nil {
				call.Err = err
			}
			call.Done()
		}
	}
	// 需不需要加入关闭连接？
	c.terminateCalls(err)
}

// Go 再添加一个同步请求
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		logger.Logger.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		done:          done,
	}
	c.send(call)
	return call
}

func (c *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	// 根据传入的参数生成一个调用call 再把call发送过去
	call := c.Go(serviceMethod, args, reply, make(chan *Call, 1)).done
	select {
	case call := <-call:
		return call.Err
	case <-ctx.Done():
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	}
}

type clientResult struct {
	client *Client
	err    error
}

func Dial(network, address string, opts ...*option.Option) (*Client, error) {
	return dialTimeout(NewGobClient, network, address, opts...)
}

func DialHTTP(network, address string, opts ...*option.Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

func XDial(rpcAddr string, opts ...*option.Option) (*Client, error) {
	r := strings.Split(rpcAddr, "@")
	if len(r) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	proto := r[0]
	address := r[1]
	switch proto {
	case "http":
		return DialHTTP("tcp", address, opts...)
	default:
		return Dial(proto, address, opts...)
	}
}

// dialTimeout 客户端发起请求 这个函数很奇怪 个人感觉应该绑定在Client结构体上才对
// 新增处理连接超时
func dialTimeout(newClient NewClient, network, address string, opts ...*option.Option) (*Client, error) {
	opt := parseOptions(opts...)
	if opt == nil {
		logger.Logger.Println("parse opts fail")
		return nil, errors.New("parse opts fail")
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeOut)
	if err != nil {
		logger.Logger.Println("dail to server fail:err:", err)
		return nil, err
	}
	defer func() {
		// 如果有错误的话即时关闭连接
		if err != nil {
			_ = conn.Close()
		}
	}()
	// 定义一个通道
	var ch = make(chan *clientResult)
	go func() {
		client, err := newClient(conn, opt)
		ch <- &clientResult{client: client, err: err}
	}()
	// 0 means no limit 0意味着没有限制，因此直接从中读取数据
	if opt.ConnectTimeOut == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case result := <-ch:
		return result.client, result.err
	case <-time.After(opt.ConnectTimeOut):
		return nil, errors.New("client get response timeout")
	}
}

type NewClient func(conn net.Conn, opt *option.Option) (*Client, error)

func NewGobClient(conn net.Conn, opt *option.Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		logger.Logger.Println("UnSupported Codec Type")
		return nil, errors.New("UnSupported Codec Type")
	}
	err := json.NewEncoder(conn).Encode(opt)
	if err != nil {
		logger.Logger.Println("EnCode opt fail")
		_ = conn.Close()
		return nil, errors.New("EnCode opt fail")
	}
	client := &Client{
		Codec:    f(conn),
		Seq:      0,
		Pending:  make(map[uint64]*Call, 10),
		Opt:      opt,
		mu:       new(sync.Mutex),
		sending:  new(sync.Mutex),
		Closing:  false,
		ShutDown: false,
	}
	go client.receive()
	return client, nil
}

func NewJsonClient(conn net.Conn, opt *option.Option) *Client {
	f := codec.NewCodecFuncMap[codec.JsonType]
	return &Client{
		Codec:    f(conn),
		Seq:      0,
		Pending:  make(map[uint64]*Call, 10),
		Opt:      opt,
		mu:       new(sync.Mutex),
		sending:  new(sync.Mutex),
		Closing:  false,
		ShutDown: false,
	}
}

func NewHTTPClient(conn net.Conn, opt *option.Option) (*Client, error) {
	// 建立connect 连接
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	//conn.Write([]byte(fmt.Sprint("CONNECT %s HTTP 1.0/n/n", defaultRPCPath)))
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err != nil {
		logger.Logger.Println("get http response fail:err:", err)
		return nil, err
	}

	if response.Status == connected {
		logger.Logger.Println("get http response success")
		return NewGobClient(conn, opt)
	}
	return nil, errors.New("unexpected HTTP response:" + response.Status)
}

func parseOptions(opts ...*option.Option) *option.Option {
	if len(opts) == 0 || opts[0] == nil {
		return option.DefaultOption
	}
	if len(opts) != 1 {
		logger.Logger.Println("option num is more than one")
		return nil
	}
	opt := opts[0]
	opt.MagicNumber = option.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = codec.GobType
	}
	return opt
}

// 对于调用而言 需要 注册调用 删除调用 终止调动 这三个函数 返回序列号有什么用？
func (c *Client) registerCall(call *Call) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.IsValid() {
		logger.Logger.Println("register fail,the conn is closed")
		return 0, errors.New("register fail,the conn is closed")
	}
	call.Seq = c.Seq
	c.Pending[c.Seq] = call
	c.Seq++
	return call.Seq, nil
}

// 返回调用有什么用？
func (c *Client) removeCall(seq uint64) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.Pending[seq]
	delete(c.Pending, seq)
	return call
}

// 终止客户端当前所有调用 并返回err信息 这一般是在发生了错误的时候才执行
func (c *Client) terminateCalls(err error) {
	// 这两行感觉没有必要加了
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ShutDown = true
	for _, call := range c.Pending {
		// 将错误发给所有的调用
		call.Err = err
		call.Done()
	}
}

// Call 然后要定义发送请求的Call Call中需要有指示开始结束的字段 存储函数调用的结果
type Call struct {
	ServiceMethod string      // 需要调用的服务方法
	Args          interface{} // 需要调用的参数
	Seq           uint64      // 调用的序列号
	done          chan *Call  // 调用管道 如果调用完成的话，将自己放入管道中去
	Err           error       // 记录调用过程中的错误
	Reply         interface{} // 调用返回值
}

func (c *Call) Done() {
	c.done <- c
}
