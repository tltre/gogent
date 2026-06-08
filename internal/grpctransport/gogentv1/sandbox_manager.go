// Package gogentv1 provides the SandboxManager gRPC service for App-side
// sandbox lifecycle management and command execution.
//
// The service uses structpb.Struct as the wire format for all messages,
// with typed Go structs used internally by client and server implementations.
// This avoids requiring protoc code generation while maintaining full gRPC
// compatibility.
package gogentv1

import (
	context "context"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// ---------------------------------------------------------------------------
// Service descriptor
// ---------------------------------------------------------------------------

const (
	SandboxManager_ListSandboxes_FullMethodName  = "/gogent.v1.SandboxManager/ListSandboxes"
	SandboxManager_CreateSandbox_FullMethodName  = "/gogent.v1.SandboxManager/CreateSandbox"
	SandboxManager_GetSandbox_FullMethodName     = "/gogent.v1.SandboxManager/GetSandbox"
	SandboxManager_DestroySandbox_FullMethodName = "/gogent.v1.SandboxManager/DestroySandbox"
	SandboxManager_ExecInSandbox_FullMethodName  = "/gogent.v1.SandboxManager/ExecInSandbox"
)

// SandboxManagerServiceDesc is the grpc.ServiceDesc for SandboxManager service.
var SandboxManagerServiceDesc = grpc.ServiceDesc{
	ServiceName: "gogent.v1.SandboxManager",
	HandlerType: (*SandboxManagerServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "ListSandboxes", Handler: _SandboxManager_ListSandboxes_Handler},
		{MethodName: "CreateSandbox", Handler: _SandboxManager_CreateSandbox_Handler},
		{MethodName: "GetSandbox", Handler: _SandboxManager_GetSandbox_Handler},
		{MethodName: "DestroySandbox", Handler: _SandboxManager_DestroySandbox_Handler},
		{MethodName: "ExecInSandbox", Handler: _SandboxManager_ExecInSandbox_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "sandbox_manager.proto",
}

// ---------------------------------------------------------------------------
// Handlers — all messages use *structpb.Struct for protobuf wire serialization
// ---------------------------------------------------------------------------

func _SandboxManager_ListSandboxes_Handler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &structpb.Struct{}
	if err := dec(in); err != nil {
		return nil, err
	}
	_ = in
	server := srv.(SandboxManagerServer)
	result, err := server.ListSandboxes(ctx, &ListSandboxesRequest{})
	if err != nil {
		return nil, err
	}
	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		items[i] = map[string]any{
			"name":    item.Name,
			"profile": item.Profile,
			"status":  item.Status,
		}
	}
	return structpb.NewStruct(map[string]any{"items": items})
}

func _SandboxManager_CreateSandbox_Handler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &structpb.Struct{}
	if err := dec(in); err != nil {
		return nil, err
	}
	req := &CreateSandboxRequest{
		Name:      in.Fields["name"].GetStringValue(),
		Profile:   in.Fields["profile"].GetStringValue(),
		Lifecycle: in.Fields["lifecycle"].GetStringValue(),
		TimeoutMs: int64(in.Fields["timeout_ms"].GetNumberValue()),
	}
	result, err := srv.(SandboxManagerServer).CreateSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(map[string]any{
		"name": result.Name, "profile": result.Profile, "status": result.Status,
	})
}

func _SandboxManager_GetSandbox_Handler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &structpb.Struct{}
	if err := dec(in); err != nil {
		return nil, err
	}
	req := &GetSandboxRequest{SandboxName: in.Fields["sandbox_name"].GetStringValue()}
	result, err := srv.(SandboxManagerServer).GetSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(map[string]any{
		"name": result.Name, "profile": result.Profile, "status": result.Status,
	})
}

func _SandboxManager_DestroySandbox_Handler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &structpb.Struct{}
	if err := dec(in); err != nil {
		return nil, err
	}
	req := &DestroySandboxRequest{SandboxName: in.Fields["sandbox_name"].GetStringValue()}
	result, err := srv.(SandboxManagerServer).DestroySandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(map[string]any{"success": result.Success})
}

func _SandboxManager_ExecInSandbox_Handler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &structpb.Struct{}
	if err := dec(in); err != nil {
		return nil, err
	}
	f := in.Fields
	env := make(map[string]string)
	if es := f["env"].GetStructValue(); es != nil {
		for k, v := range es.Fields {
			env[k] = v.GetStringValue()
		}
	}
	req := &ExecInSandboxRequest{
		SandboxName: f["sandbox_name"].GetStringValue(),
		Code:        f["code"].GetStringValue(),
		Language:    f["language"].GetStringValue(),
		TimeoutMs:   int64(f["timeout_ms"].GetNumberValue()),
		Env:         env,
	}
	result, err := srv.(SandboxManagerServer).ExecInSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(map[string]any{
		"stdout": result.Stdout, "stderr": result.Stderr,
		"exit_code": result.ExitCode, "error": result.Error,
	})
}

// ---------------------------------------------------------------------------
// Server interface
// ---------------------------------------------------------------------------

type SandboxManagerServer interface {
	ListSandboxes(context.Context, *ListSandboxesRequest) (*SandboxInfoList, error)
	CreateSandbox(context.Context, *CreateSandboxRequest) (*SandboxInfo, error)
	GetSandbox(context.Context, *GetSandboxRequest) (*SandboxInfo, error)
	DestroySandbox(context.Context, *DestroySandboxRequest) (*DestroySandboxResponse, error)
	ExecInSandbox(context.Context, *ExecInSandboxRequest) (*ExecResult, error)
}

type UnimplementedSandboxManagerServer struct{}

func (UnimplementedSandboxManagerServer) ListSandboxes(context.Context, *ListSandboxesRequest) (*SandboxInfoList, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSandboxes not implemented")
}
func (UnimplementedSandboxManagerServer) CreateSandbox(context.Context, *CreateSandboxRequest) (*SandboxInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSandbox not implemented")
}
func (UnimplementedSandboxManagerServer) GetSandbox(context.Context, *GetSandboxRequest) (*SandboxInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSandbox not implemented")
}
func (UnimplementedSandboxManagerServer) DestroySandbox(context.Context, *DestroySandboxRequest) (*DestroySandboxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DestroySandbox not implemented")
}
func (UnimplementedSandboxManagerServer) ExecInSandbox(context.Context, *ExecInSandboxRequest) (*ExecResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecInSandbox not implemented")
}

func RegisterSandboxManagerServer(s grpc.ServiceRegistrar, srv SandboxManagerServer) {
	s.RegisterService(&SandboxManagerServiceDesc, srv)
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

type SandboxManagerClient interface {
	ListSandboxes(ctx context.Context, in *ListSandboxesRequest, opts ...grpc.CallOption) (*SandboxInfoList, error)
	CreateSandbox(ctx context.Context, in *CreateSandboxRequest, opts ...grpc.CallOption) (*SandboxInfo, error)
	GetSandbox(ctx context.Context, in *GetSandboxRequest, opts ...grpc.CallOption) (*SandboxInfo, error)
	DestroySandbox(ctx context.Context, in *DestroySandboxRequest, opts ...grpc.CallOption) (*DestroySandboxResponse, error)
	ExecInSandbox(ctx context.Context, in *ExecInSandboxRequest, opts ...grpc.CallOption) (*ExecResult, error)
}

type sandboxManagerClient struct {
	cc grpc.ClientConnInterface
}

func NewSandboxManagerClient(cc grpc.ClientConnInterface) SandboxManagerClient {
	return &sandboxManagerClient{cc}
}

func (c *sandboxManagerClient) ListSandboxes(ctx context.Context, in *ListSandboxesRequest, opts ...grpc.CallOption) (*SandboxInfoList, error) {
	out := &structpb.Struct{}
	err := c.cc.Invoke(ctx, SandboxManager_ListSandboxes_FullMethodName, &structpb.Struct{}, out, opts...)
	if err != nil {
		return nil, err
	}
	result := &SandboxInfoList{}
	if items := out.Fields["items"].GetListValue(); items != nil {
		for _, item := range items.Values {
			if s := item.GetStructValue(); s != nil {
				result.Items = append(result.Items, &SandboxInfo{
					Name:    s.Fields["name"].GetStringValue(),
					Profile: s.Fields["profile"].GetStringValue(),
					Status:  s.Fields["status"].GetStringValue(),
				})
			}
		}
	}
	return result, nil
}

func (c *sandboxManagerClient) CreateSandbox(ctx context.Context, in *CreateSandboxRequest, opts ...grpc.CallOption) (*SandboxInfo, error) {
	req, _ := structpb.NewStruct(map[string]any{
		"name": in.Name, "profile": in.Profile,
		"lifecycle": in.Lifecycle, "timeout_ms": in.TimeoutMs,
	})
	out := &structpb.Struct{}
	err := c.cc.Invoke(ctx, SandboxManager_CreateSandbox_FullMethodName, req, out, opts...)
	if err != nil {
		return nil, err
	}
	return &SandboxInfo{
		Name: out.Fields["name"].GetStringValue(),
		Profile: out.Fields["profile"].GetStringValue(),
		Status: out.Fields["status"].GetStringValue(),
	}, nil
}

func (c *sandboxManagerClient) GetSandbox(ctx context.Context, in *GetSandboxRequest, opts ...grpc.CallOption) (*SandboxInfo, error) {
	req, _ := structpb.NewStruct(map[string]any{"sandbox_name": in.SandboxName})
	out := &structpb.Struct{}
	err := c.cc.Invoke(ctx, SandboxManager_GetSandbox_FullMethodName, req, out, opts...)
	if err != nil {
		return nil, err
	}
	return &SandboxInfo{
		Name: out.Fields["name"].GetStringValue(),
		Profile: out.Fields["profile"].GetStringValue(),
		Status: out.Fields["status"].GetStringValue(),
	}, nil
}

func (c *sandboxManagerClient) DestroySandbox(ctx context.Context, in *DestroySandboxRequest, opts ...grpc.CallOption) (*DestroySandboxResponse, error) {
	req, _ := structpb.NewStruct(map[string]any{"sandbox_name": in.SandboxName})
	out := &structpb.Struct{}
	err := c.cc.Invoke(ctx, SandboxManager_DestroySandbox_FullMethodName, req, out, opts...)
	if err != nil {
		return nil, err
	}
	return &DestroySandboxResponse{Success: out.Fields["success"].GetBoolValue()}, nil
}

func (c *sandboxManagerClient) ExecInSandbox(ctx context.Context, in *ExecInSandboxRequest, opts ...grpc.CallOption) (*ExecResult, error) {
	envAny := make(map[string]any, len(in.Env))
	for k, v := range in.Env {
		envAny[k] = v
	}
	envStruct, _ := structpb.NewStruct(envAny)
	req, _ := structpb.NewStruct(map[string]any{
		"sandbox_name": in.SandboxName, "code": in.Code,
		"language": in.Language, "timeout_ms": in.TimeoutMs,
		"env": envStruct,
	})
	out := &structpb.Struct{}
	err := c.cc.Invoke(ctx, SandboxManager_ExecInSandbox_FullMethodName, req, out, opts...)
	if err != nil {
		return nil, err
	}
	return &ExecResult{
		Stdout: out.Fields["stdout"].GetStringValue(),
		Stderr: out.Fields["stderr"].GetStringValue(),
		ExitCode: int32(out.Fields["exit_code"].GetNumberValue()),
		Error: out.Fields["error"].GetStringValue(),
	}, nil
}
