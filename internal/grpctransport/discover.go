package grpctransport

import (
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ComponentInfo describes a discovered gRPC service and its available RPC methods.
type ComponentInfo struct {
	ServiceName string   // Full service name, e.g. "gogent.v1.ProviderService"
	Methods     []string // RPC method names, e.g. ["Generate", "ModelInfo"]
}

// ComponentType extracts the component type from the service name.
// Example: "gogent.v1.ProviderService" → "provider"
func (c ComponentInfo) ComponentType() string {
	name := c.ServiceName
	// Remove package prefix (e.g. "gogent.v1.")
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	// Remove "Service" suffix
	name = strings.TrimSuffix(name, "Service")
	return strings.ToLower(name)
}

// Discover uses gRPC server reflection to discover services and their methods
// on the given connection.
func Discover(ctx context.Context, conn *grpc.ClientConn) ([]ComponentInfo, error) {
	client := grpc_reflection_v1.NewServerReflectionClient(conn)

	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("grpctransport: discover: %w", err)
	}

	// List all services.
	if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{
			ListServices: "*",
		},
	}); err != nil {
		return nil, fmt.Errorf("grpctransport: list services: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("grpctransport: list services recv: %w", err)
	}

	listResp := resp.GetListServicesResponse()
	if listResp == nil {
		return nil, fmt.Errorf("grpctransport: unexpected response type for list services")
	}

	var results []ComponentInfo
	for _, svc := range listResp.GetService() {
		info := ComponentInfo{
			ServiceName: svc.GetName(),
		}

		// Request file descriptor containing this service to extract method names.
		methods, err := discoverMethods(ctx, stream, svc.GetName())
		if err != nil {
			// Non-fatal: log and continue with empty methods list.
			info.Methods = nil
		} else {
			info.Methods = methods
		}

		results = append(results, info)
	}

	stream.CloseSend()
	return results, nil
}

// discoverMethods requests the file descriptor for a given symbol (service name)
// and extracts its RPC method names.
func discoverMethods(_ context.Context, stream grpc_reflection_v1.ServerReflection_ServerReflectionInfoClient, symbol string) ([]string, error) {
	if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: symbol,
		},
	}); err != nil {
		return nil, fmt.Errorf("send file_containing_symbol %q: %w", symbol, err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("recv file_containing_symbol %q: %w", symbol, err)
	}

	fdResp := resp.GetFileDescriptorResponse()
	if fdResp == nil {
		return nil, fmt.Errorf("unexpected response type for file_containing_symbol %q", symbol)
	}

	// Extract the short service name (e.g. "ProviderService" from "gogent.v1.ProviderService").
	shortName := symbol
	if idx := strings.LastIndex(symbol, "."); idx >= 0 {
		shortName = symbol[idx+1:]
	}

	for _, fdBytes := range fdResp.GetFileDescriptorProto() {
		var fd descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(fdBytes, &fd); err != nil {
			continue
		}

		for _, svcDesc := range fd.GetService() {
			if svcDesc.GetName() == shortName {
				methods := make([]string, 0, len(svcDesc.GetMethod()))
				for _, method := range svcDesc.GetMethod() {
					methods = append(methods, method.GetName())
				}
				return methods, nil
			}
		}
	}

	return nil, fmt.Errorf("service %q not found in file descriptors", symbol)
}

// DiscoverServices is a convenience wrapper that returns service names only,
// without method details. It is faster because it skips file descriptor requests.
func DiscoverServices(ctx context.Context, conn *grpc.ClientConn) ([]string, error) {
	client := grpc_reflection_v1.NewServerReflectionClient(conn)

	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("grpctransport: discover services: %w", err)
	}
	defer func() {
		stream.CloseSend()
		// Drain until EOF to release the stream cleanly.
		for {
			if _, err := stream.Recv(); err != nil {
				break
			}
		}
	}()

	if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{
			ListServices: "*",
		},
	}); err != nil {
		return nil, fmt.Errorf("grpctransport: list services: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("grpctransport: list services recv: %w", err)
	}

	listResp := resp.GetListServicesResponse()
	if listResp == nil {
		return nil, fmt.Errorf("grpctransport: unexpected response type for list services")
	}

	names := make([]string, 0, len(listResp.GetService()))
	for _, svc := range listResp.GetService() {
		names = append(names, svc.GetName())
	}
	return names, nil
}
