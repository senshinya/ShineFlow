package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var httpRequestType = &NodeType{
	Key:         BuiltinHTTPRequest,
	Version:     NodeTypeVersion1,
	Name:        "HTTP Request",
	Description: "通过 ExecServices.HTTPClient 发起出站 HTTP；2xx/3xx 走 default，4xx/5xx 走 error。",
	Category:    CategoryTool,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
