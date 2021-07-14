/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/6/30 21:11
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package option

import (
	"rpc/codec"
	"time"
)

const MagicNumber = 0x123456

type Option struct {
	MagicNumber    uint64        // 魔法数字
	CodecType      string        // 编码器的类型
	ConnectTimeOut time.Duration // 连接超时时间
	HandleTimeOut  time.Duration
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
} // 默认选项 方便用户使用
