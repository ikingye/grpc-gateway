package gengateway

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/gengo/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
)

type param struct {
	*descriptor.File
	Imports []descriptor.GoPackage
}

func applyTemplate(p param) (string, error) {
	w := bytes.NewBuffer(nil)
	if err := headerTemplate.Execute(w, p); err != nil {
		return "", err
	}
	var methodSeen bool
	for _, svc := range p.Services {
		for _, meth := range svc.Methods {
			methodSeen = true
			if err := handlerTemplate.Execute(w, meth); err != nil {
				return "", err
			}
		}
	}
	if !methodSeen {
		return "", errNoTargetService
	}
	if err := trailerTemplate.Execute(w, p.Services); err != nil {
		return "", err
	}
	return w.String(), nil
}

var (
	headerTemplate = template.Must(template.New("header").Parse(`
// Code generated by protoc-gen-grpc-gateway
// source: {{.GetName}}
// DO NOT EDIT!

/*
Package {{.GoPkg.Name}} is a reverse proxy.

It translates gRPC into RESTful JSON APIs.
*/
package {{.GoPkg.Name}}
import (
	{{range $i := .Imports}}{{if $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}

	{{range $i := .Imports}}{{if not $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}
)

var _ codes.Code
var _ io.Reader
var _ = runtime.String
`))

	handlerTemplate = template.Must(template.New("handler").Parse(`
{{if .GetClientStreaming}}
{{template "client-streaming-request-func" .}}
{{else}}
{{template "client-rpc-request-func" .}}
{{end}}
`))

	_ = template.Must(handlerTemplate.New("request-func-signature").Parse(strings.Replace(`
{{if .GetServerStreaming}}
func request_{{.Service.GetName}}_{{.GetName}}(ctx context.Context, client {{.Service.GetName}}Client, req *http.Request, pathParams map[string]string) ({{.Service.GetName}}_{{.GetName}}Client, error)
{{else}}
func request_{{.Service.GetName}}_{{.GetName}}(ctx context.Context, client {{.Service.GetName}}Client, req *http.Request, pathParams map[string]string) (msg proto.Message, err error)
{{end}}`, "\n", "", -1)))

	_ = template.Must(handlerTemplate.New("client-streaming-request-func").Parse(`
{{template "request-func-signature" .}} {
	stream, err := client.{{.GetName}}(ctx)
	if err != nil {
		glog.Errorf("Failed to start streaming: %v", err)
		return nil, err
	}
	dec := {{.Body.DecoderFactoryExpr}}(req.Body)
	for {
		var protoReq {{.RequestType.GoType .Service.File.GoPkg.Path}}
		err = dec.Decode(&protoReq)
		if err == io.EOF {
			break
		}
		if err != nil {
			glog.Errorf("Failed to decode request: %v", err)
			return nil, grpc.Errorf(codes.InvalidArgument, "%v", err)
		}
		if err = stream.Send(&protoReq); err != nil {
			glog.Errorf("Failed to send request: %v", err)
			return nil, err
		}
	}
{{if .GetServerStreaming}}
	if err = stream.CloseSend(); err != nil {
		glog.Errorf("Failed to terminate client stream: %v", err)
		return nil, err
	}
	return stream, nil
{{else}}
	return stream.CloseAndRecv()
{{end}}
}
`))

	_ = template.Must(handlerTemplate.New("client-rpc-request-func").Parse(`
{{template "request-func-signature" .}} {
	var protoReq {{.RequestType.GoType .Service.File.GoPkg.Path}}
	{{range $param := .QueryParams}}
	protoReq.{{$param.RHS "protoReq"}}, err = {{$param.ConvertFuncExpr}}(req.FormValue({{$param | printf "%q"}}))
	if err != nil {
		return nil, err
	}
	{{end}}
{{if .Body}}
	if err = {{.Body.DecoderFactoryExpr}}(req.Body).Decode(&{{.Body.RHS "protoReq"}}); err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "%v", err)
	}
{{end}}
{{if .PathParams}}
	var val string
	var ok bool
	{{range $param := .PathParams}}
	val, ok = pathParams[{{$param | printf "%q"}}]
	if !ok {
		return nil, grpc.Errorf(codes.InvalidArgument, "missing parameter %s", {{$param | printf "%q"}})
	}
	{{$param.RHS "protoReq"}}, err = {{$param.ConvertFuncExpr}}(val)
	if err != nil {
		return nil, err
	}
	{{end}}
{{end}}

	return client.{{.GetName}}(ctx, &protoReq)
}`))

	trailerTemplate = template.Must(template.New("trailer").Parse(`
{{range $svc := .}}
// Register{{$svc.GetName}}HandlerFromEndpoint is same as Register{{$svc.GetName}}Handler but
// automatically dials to "endpoint" and closes the connection when "ctx" gets done.
func Register{{$svc.GetName}}HandlerFromEndpoint(ctx context.Context, mux *runtime.ServeMux, endpoint string) (err error) {
	conn, err := grpc.Dial(endpoint)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if cerr := conn.Close(); cerr != nil {
				glog.Error("Failed to close conn to %s: %v", endpoint, cerr)
			}
			return
		}
		go func() {
			<-ctx.Done()
			if cerr := conn.Close(); cerr != nil {
				glog.Error("Failed to close conn to %s: %v", endpoint, cerr)
			}
		}()
	}()

	return Register{{$svc.GetName}}Handler(ctx, mux, conn)
}

// Register{{$svc.GetName}}Handler registers the http handlers for service {{$svc.GetName}} to "mux".
// The handlers forward requests to the grpc endpoint over "conn".
func Register{{$svc.GetName}}Handler(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	client := New{{$svc.GetName}}Client(conn)
	{{range $m := $svc.Methods}}
	mux.Handle({{$m.HTTPMethod | printf "%q"}}, pattern_{{$svc.GetName}}_{{$m.GetName}}, func(w http.ResponseWriter, req *http.Request, pathParams map[string]string) {
		resp, err := request_{{$svc.GetName}}_{{$m.GetName}}(ctx, client, req, pathParams)
		if err != nil {
			runtime.HTTPError(w, err)
			return
		}
		{{if $m.GetServerStreaming}}
		runtime.ForwardResponseStream(w, func() (proto.Message, error) { return resp.Recv() })
		{{else}}
		runtime.ForwardResponseMessage(w, resp)
		{{end}}
	})
	{{end}}
	return nil
}

var (
	{{range $m := $svc.Methods}}
	pattern_{{$svc.GetName}}_{{$m.GetName}} = runtime.MustPattern(runtime.NewPattern({{$m.PathTmpl.Version}}, {{$m.PathTmpl.OpCodes | printf "%#v"}}, {{$m.PathTmpl.Pool | printf "%#v"}}, {{$m.PathTmpl.Verb | printf "%q"}}))
	{{end}}
)
{{end}}`))
)
