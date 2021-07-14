/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/6/30 21:10
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"rpc/codec"
	"rpc/logger"
	"rpc/option"
	"rpc/service"
	"strings"
	"sync"
	"time"
)

const (
	connected        = "200 Connected to Gee RPC"
	defaultRPCPath   = "/_rpc_"
	defaultDebugPath = "/debug/rpc"
)

type Server struct {
	ServiceMap *sync.Map // 段锁map
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 如果连接不是CONNECT连接的话
	if req.Method != "CONNECT" {
		// text/plain的意思是将文件设置为纯文本的形式，浏览器在获取到这种文件时并不会对其进行处理。
		w.Header().Set("Content-Type", "text/plain; charset=utf-8") // 设置编码格式
		// 返回状态不被允许的 状态码
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		//w.Write([]byte("405 must CONNECT\n"))
		return
	}
	// 劫持HTTP连接 转换成TCP conn
	conn, _, err := w.(http.Hijacker).Hijack()
	// 劫持失败 返回错误信息
	if err != nil {
		logger.Logger.Println("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	// 回送连接成功消息
	// 这里填入的是conn而不是w
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	// 转成rpc通信
	s.serveConn(conn)
}

func HandleHTTP() {
	http.Handle(defaultRPCPath, DefaultServer)
	http.Handle(defaultDebugPath, debugHTTP{DefaultServer})
	logger.Logger.Println("rpc server debug path:", defaultDebugPath)
}

func (s *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, s)
	http.Handle(defaultDebugPath, debugHTTP{s})
	logger.Logger.Println("rpc server debug path:", defaultDebugPath)
}

func NewServer() *Server {
	return &Server{
		ServiceMap: new(sync.Map), // 初始化
	}
}

func (s *Server) RegisterService(ins interface{}) {
	service := service.NewService(ins)              // 注册服务
	s.ServiceMap.LoadOrStore(service.Name, service) // 载入全局MAP
}

func RegisterService(ins interface{}) {
	service := service.NewService(ins)
	// 注册服务
	DefaultServer.ServiceMap.LoadOrStore(service.Name, service) // 载入全局MAP
}

// 发现服务
func (s *Server) findService(serviceMethod string) (*service.Service, *service.Method, error) {
	strArr := strings.Split(serviceMethod, ".")
	if len(strArr) != 2 {
		logger.Logger.Println("the format of serviceMethod is wrong")
		return nil, nil, errors.New("the format of serviceMethod is wrong")
	}
	if val, ok := s.ServiceMap.Load(strArr[0]); ok {
		service := val.(*service.Service)
		method := service.Methods[strArr[1]]
		if method == nil {
			logger.Logger.Println("rpc server: can't find method " + strArr[1])
			return nil, nil, errors.New("rpc server: can't find method " + strArr[1])
		}
		return service, method, nil
	}
	logger.Logger.Println("the service is not registered")
	return nil, nil, errors.New("the service is not registered")
}

var DefaultServer = NewServer()

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			logger.Logger.Fatal("listener get conn fail,exit")
		}
		go s.serveConn(conn) // 单独开启一个协程处理该连接的请求
	}
}

func (s *Server) serveConn(conn net.Conn) {
	decoder := json.NewDecoder(conn) // 封装conn为一个json解码器
	var opt option.Option            // 读取出数据并将能够解析的第一个json进行解析成结构体
	err := decoder.Decode(&opt)
	if err != nil {
		logger.Logger.Println("parse json to option fail") // 解析失败直接退出
		return
	}
	if opt.MagicNumber != opt.MagicNumber {
		logger.Logger.Println("MagicNumber is false") // 解析失败直接退出
		return
	}
	if f, ok := codec.NewCodecFuncMap[opt.CodecType]; ok {
		// FIXME
		s.serveCodec(f(conn), &opt)
		return
	} else {
		logger.Logger.Println("UnSupported Codec Type")
		return
	}
}

var invalidRequest = struct{}{}

func (s *Server) serveCodec(c codec.Codec, opt *option.Option) {
	// 只传输一个option 后面接上多个 header和body是有可能的
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	// 代码会一次性读取出多个请求 然后退出for循环 卡在wait上 等所有的请求全部处理完毕再退出函数
	for {
		request, err := s.readRequest(c)
		if err != nil {
			if request == nil {
				// 如果请求为空说明header就没有解析成功 即时让循环重新再来一次 后面的请求也不可能解析成功
				break
			}
			request.header.Err = err.Error()
			s.sendResponse(c, request.header, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go s.handleRequest(c, request, sending, wg, opt.HandleTimeOut)
	}
	wg.Wait()
	_ = c.Close()
}

type Request struct {
	header      *codec.Header //请求头
	args, reply reflect.Value // 参数和回复 反射值
	service     *service.Service
	methodName  string
}

func (s *Server) readRequestHeader(c codec.Codec) (*codec.Header, error) {
	var header codec.Header
	err := c.ReadHeader(&header)
	if err != nil {
		// EOF 说明数据已经读完了
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &header, nil
}

func (s *Server) readRequest(c codec.Codec) (*Request, error) {
	header, err := s.readRequestHeader(c)
	if err != nil {
		//logger.Logger.Println("read header fail,err:", err)
		return nil, err
	}
	request := &Request{header: header}
	service, methodType, err := s.findService(header.ServiceMethod)
	if service == nil || methodType == nil {
		logger.Logger.Println(" find service fail err:", err)
		return request, err
	}
	request.args = methodType.NewArgs()
	request.reply = methodType.NewReply()
	request.service = service
	request.methodName = methodType.Method.Name
	//request.args = reflect.New(reflect.TypeOf(""))
	argsi := request.args.Interface()
	if request.args.Type().Kind() != reflect.Ptr {
		argsi = request.args.Addr().Interface()
	}
	// 读取到argsi中 request中的args数值也相应改变了
	err = c.ReadBody(argsi)
	if err != nil {
		logger.Logger.Println(" read args err:", err)
		// header解析出来了 但是body解析错误 这种情况下爱仍然可以继续处理请求
		return request, err
	}
	return request, nil
}

func (s *Server) handleRequest(c codec.Codec, request *Request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	ch := make(chan struct{})
	send := make(chan struct{})
	go func() {
		err := request.service.Call(request.methodName, request.args, request.reply)
		ch <- struct{}{}
		if err != nil {
			request.header.Err = err.Error()
		}
		s.sendResponse(c, request.header, request.reply.Interface(), sending)
		send <- struct{}{}
	}()
	// FIXME
	if timeout == 0 {
		<-ch
		<-send
		return
	}
	select {
	// FIXME
	case <-time.After(timeout):
		request.header.Err = fmt.Sprintf("rpc server: request handle timeout: expect within 1")
		s.sendResponse(c, request.header, invalidRequest, sending)
		return
	case <-ch:
		<-send
	}

}

func (s *Server) sendResponse(c codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := c.Write(h, body); err != nil {
		logger.Logger.Println("rpc server: write response error:", err)
	}
}
