/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/6/30 16:19
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"net"
	"rpc/logger"
)

type Header struct {
	ServiceMethod string // 需要获取的服务模块名+函数名
	Seq           uint64 // 请求的序列号
	Err           string //请求过程中的错误信息
}

type NewCodecFunc func(conn net.Conn) Codec

// Codec 定义编码器接口
type Codec interface {
	io.Closer
	ReadHeader(header *Header) error
	ReadBody(body interface{}) error
	Write(header *Header, body interface{}) error
}

// NewCodecFuncMap 全局构造函数MAP
var NewCodecFuncMap = make(map[string]NewCodecFunc)

const (
	GobType  = "GobType"
	JsonType = "JsonType"
)

func init() {
	NewCodecFuncMap[GobType] = NewGobCodec
}

type GobCodec struct {
	conn io.ReadWriteCloser //只要满足读写关闭函数的连接都可以传入 不仅仅是TCP的conn连接
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func NewGobCodec(conn net.Conn) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (g *GobCodec) ReadHeader(header *Header) error {
	return g.dec.Decode(header)
}

func (g *GobCodec) ReadBody(body interface{}) error {
	return g.dec.Decode(body)
}

func (g *GobCodec) Close() error {
	return g.conn.Close()
}

func (g *GobCodec) Write(header *Header, body interface{}) (err error) {
	if err = g.enc.Encode(header); err != nil {
		logger.Logger.Println("encode header fail!")
		return
	}
	if err = g.enc.Encode(body); err != nil {
		logger.Logger.Println("encode body fail!")
		return
	}
	defer func() {
		_ = g.buf.Flush()
		if err != nil {
			logger.Logger.Println("write data fail!")
			_ = g.conn.Close()
			return
		}
	}()
	return nil
}
