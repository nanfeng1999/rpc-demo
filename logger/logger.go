/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/6/30 21:24
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package logger

import (
	"log"
	"os"
)

var Logger = log.New(os.Stdout, "", log.Lshortfile|log.LstdFlags)
