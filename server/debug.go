/**
 * @Author: yzy
 * @Description:
 * @Version: 1.0.0
 * @Date: 2021/7/10 21:05
 * @Copyright: MIN-Group；国家重大科技基础设施——未来网络北大实验室；深圳市信息论与未来网络重点实验室
 */
package server

import (
	"fmt"
	"html/template"
	"net/http"
	"rpc/service"
)

const debugText = `<html>
	<body>
	<title>GeeRPC Services</title>
	{{range .}}
	<hr>
	Service {{.Name}}
	<hr>
		<table>
		<th align=center>Method</th><th align=center>Calls</th>
		{{range $name, $mtype := .Method}}
			<tr>
			<td align=left font=fixed>{{$name}}({{$mtype.Args}}, {{$mtype.Reply}}) error</td>
			<td align=center>{{$mtype.CallNum}}</td>
			</tr>
		{{end}}
		</table>
	{{end}}
	</body>
	</html>`

var debug = template.Must(template.New("RPC debug").Parse(debugText))

type debugHTTP struct {
	*Server
}

type debugService struct {
	Name   string
	Method map[string]*service.Method
}

// Runs at /debug/geerpc
func (server debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Build a sorted version of the data.
	var services []debugService
	server.ServiceMap.Range(func(namei, svci interface{}) bool {
		svc := svci.(*service.Service)
		services = append(services, debugService{
			Name:   namei.(string),
			Method: svc.Methods,
		})
		return true
	})
	err := debug.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}
