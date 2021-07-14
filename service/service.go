/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/7 18:18
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package service

import (
	"go/ast"
	"reflect"
	"rpc/logger"
	"sync/atomic"
)

type Service struct {
	Name    string        // 服务名
	Type    reflect.Type  // 反射类型
	Value   reflect.Value // 反射值
	Methods map[string]*Method
}

type Method struct {
	Method  reflect.Method // 反射方法结构体
	Args    reflect.Type   // 方法传入的参数
	Reply   reflect.Type   // 函数的返回值
	CallNum uint64         // 这个函数被调用的次数
}

func (m *Method) CallNums() uint64 {
	return atomic.LoadUint64(&m.CallNum) // 原子操作读取某数字
}

func (m *Method) NewArgs() reflect.Value {
	// 为什么这里不需要判断是不是切片或者map类型？
	if m.Args.Kind() == reflect.Ptr {
		// 如果输入参数的类型是指针类型
		return reflect.New(m.Args.Elem()) // 返回指针类型
	} else {
		return reflect.New(m.Args).Elem() // 返回值类型
	}
}

func (m *Method) NewReply() reflect.Value {
	// 返回值必须是指针类型 因此用New
	replyv := reflect.New(m.Reply.Elem())
	// 判断元素种类
	switch m.Reply.Elem().Kind() {
	case reflect.Map: // MAP类
		replyv.Elem().Set(reflect.MakeMap(m.Reply.Elem()))
	case reflect.Slice: // 切片类
		replyv.Elem().Set(reflect.MakeSlice(m.Reply.Elem(), 0, 0))
	}
	return replyv
}

// NewService 传入一个实体 返回一个服务结构体
func NewService(val interface{}) *Service {
	var s Service
	s.Name = reflect.TypeOf(val).Elem().Name()
	s.Type = reflect.TypeOf(val)
	s.Value = reflect.ValueOf(val)
	s.registerMethod()
	return &s
}

func (s *Service) registerMethod() {
	s.Methods = make(map[string]*Method)
	for i := 0; i < s.Type.NumMethod(); i++ {
		var m Method
		method := s.Type.Method(i)     // 第i个方法名字
		mType := s.Type.Method(i).Type // 第i个方法的类型
		if mType.NumOut() != 1 || mType.NumIn() != 3 {
			logger.Logger.Println("ERRO:", "num of parameters is wrong")
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			logger.Logger.Println("ERRO:", "the type of first out is not error")
			continue
		}
		m.Args, m.Reply, m.Method = mType.In(1), mType.In(2), method // 分别赋值第一个 第二个参数的Type
		// 如果这两个参数 有不是被引入的或者是内嵌的类型 那么直接返回
		if !isExportedOrBuiltinType(m.Args) || !isExportedOrBuiltinType(m.Reply) {
			continue
		}
		s.Methods[method.Name] = &m
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	// PkgPath返回类型的包路径，即明确指定包的import路径，如"encoding/base64"
	// 如果类型为内建类型(string, error)或未命名类型(*T, struct{}, []int)，会返回""
	// 如果变量是内建类型 或者 是可以导出的变量的话 那么 返回true
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// Call 调用需要传入 函数名字 参数 返回值  error
func (s *Service) Call(name string, args reflect.Value, reply reflect.Value) error {
	m := s.Methods[name] //取出函数对应的Method
	atomic.AddUint64(&m.CallNum, 1)
	f := m.Method.Func
	out := f.Call([]reflect.Value{s.Value, args, reply})
	// 如果有错误的话转义 否则直接返回nil
	if errInter := out[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
