package triage

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func traceRequests(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if strings.HasPrefix(method, "notifications/") {
			return next(ctx, method, req)
		}
		carrier := propagation.MapCarrier{}
		if params := req.GetParams(); hasParams(params) {
			for _, key := range []string{"traceparent", "tracestate"} {
				if value, ok := params.GetMeta()[key].(string); ok {
					carrier[key] = value
				}
			}
		}
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
		name := method
		attrs := []attribute.KeyValue{attribute.String("mcp.method.name", method)}
		switch p := req.GetParams().(type) {
		case *mcp.CallToolParamsRaw:
			if p == nil {
				break
			}
			if knownTool(p.Name) {
				name += " " + p.Name
				attrs = append(attrs, attribute.String("gen_ai.tool.name", p.Name))
			}
			attrs = append(attrs, attribute.String("gen_ai.operation.name", "execute_tool"))
		case *mcp.GetPromptParams:
			if p == nil {
				break
			}
			if p.Name == "triage" {
				name += " " + p.Name
				attrs = append(attrs, attribute.String("gen_ai.prompt.name", p.Name))
			}
		}
		if caller, ok := ctx.Value(callerKey{}).(Caller); ok {
			attrs = append(attrs, attribute.String("enduser.id", caller.Name))
		}
		ctx, span := otel.Tracer("log-triage").Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
		defer span.End()
		res, err := next(ctx, method, req)
		// A URI is recorded only once the server has served it, so a URI the caller
		// made up never reaches telemetry.
		if p, ok := req.GetParams().(*mcp.ReadResourceParams); ok && p != nil && err == nil {
			span.SetAttributes(attribute.String("mcp.resource.uri", p.URI))
		}
		kind := ""
		if err != nil {
			var rpc *jsonrpc.Error
			switch {
			case errors.As(err, &rpc):
				switch rpc.Code {
				case -32700, -32600, -32601, -32602, -32002:
				default:
					kind = strconv.FormatInt(rpc.Code, 10)
				}
			case errors.Is(err, context.Canceled):
				kind = "cancelled"
			case errors.Is(err, context.DeadlineExceeded):
				kind = "timeout"
			default:
				kind = "internal_error"
			}
		} else if result, ok := res.(*mcp.CallToolResult); ok && result != nil && result.IsError {
			kind = "tool_error"
		}
		if kind != "" {
			span.SetAttributes(attribute.String("error.type", kind))
			// error.type names the failure; OpenTelemetry advises against repeating it,
			// or any caller input, in the status description.
			span.SetStatus(codes.Error, "")
		}
		return res, err
	}
}

func knownTool(name string) bool {
	switch name {
	case "list_logs", "summarize_logs", "search_logs", "group_lines", "read_lines", "timeline", "save_finding":
		return true
	}
	return false
}

// SDK requests may carry an interface containing a nil parameter pointer.
// Calling a promoted GetMeta method on it would dereference that pointer.
func hasParams(params mcp.Params) bool {
	if params == nil {
		return false
	}
	value := reflect.ValueOf(params)
	return value.Kind() != reflect.Pointer || !value.IsNil()
}
