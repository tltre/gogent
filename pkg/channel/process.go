package channel

import (
	"context"
	"fmt"
	"io"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"google.golang.org/protobuf/types/known/structpb"
)

// ProcessChannelConfig holds the configuration for a gRPC-based ProcessChannel.
type ProcessChannelConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessChannel is a channel implementation that communicates with a remote
// channel service over gRPC.
type ProcessChannel struct {
	cfg      *ProcessChannelConfig
	client   gogentv1.ChannelServiceClient
	msgChan  chan Message
	stopChan chan struct{}
}

// NewProcessChannel creates a new ProcessChannel. The gRPC client is lazily
// initialized on the first method call.
func NewProcessChannel(cfg *ProcessChannelConfig) *ProcessChannel {
	return &ProcessChannel{
		cfg:      cfg,
		msgChan:  make(chan Message, 100),
		stopChan: make(chan struct{}),
	}
}

// Name returns the channel name from config.
func (c *ProcessChannel) Name() string {
	return c.cfg.Name
}

// Start is a no-op for the gRPC-backed channel.
func (c *ProcessChannel) Start(ctx context.Context) error { return nil }

// Stop signals the receive goroutine to exit and closes the message channel.
func (c *ProcessChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return nil
}

// getClient lazily initializes and returns the gRPC ChannelServiceClient.
func (c *ProcessChannel) getClient() (gogentv1.ChannelServiceClient, error) {
	if c.client != nil {
		return c.client, nil
	}
	conn, err := c.cfg.Pool.Get(c.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("channel: get connection: %w", err)
	}
	c.client = gogentv1.NewChannelServiceClient(conn.ClientConn())
	return c.client, nil
}

// Receive returns a channel of incoming messages. On first call, it starts a
// background goroutine that opens a gRPC server-stream and forwards messages.
func (c *ProcessChannel) Receive(ctx context.Context) (<-chan Message, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Start the stream receiver goroutine once.
	select {
	case <-c.stopChan:
		return nil, fmt.Errorf("channel: already stopped")
	default:
	}

	stream, err := client.Receive(ctx, &gogentv1.ReceiveRequest{})
	if err != nil {
		return nil, fmt.Errorf("channel: open receive stream: %w", err)
	}

	go c.runReceiveStream(ctx, stream)

	return c.msgChan, nil
}

// runReceiveStream reads from the gRPC server-stream and forwards messages to msgChan.
// It exits when the stream ends, the context is cancelled, or stopChan is closed.
func (c *ProcessChannel) runReceiveStream(ctx context.Context, stream gogentv1.ChannelService_ReceiveClient) {
	defer close(c.msgChan)

	for {
		pb, err := stream.Recv()
		if err != nil {
			// io.EOF means the server closed the stream normally.
			if err == io.EOF {
				return
			}
			// Context cancellation is expected on Stop.
			if ctx.Err() != nil {
				return
			}
			return
		}

		msg := channelMessageFromProto(pb)

		select {
		case c.msgChan <- msg:
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Send pushes a message through the channel via gRPC.
func (c *ProcessChannel) Send(ctx context.Context, msg Message) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	_, err = client.Send(ctx, messageToSendRequest(msg))
	return err
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func messageToSendRequest(msg Message) *gogentv1.SendRequest {
	return &gogentv1.SendRequest{
		Id:        msg.ID,
		SessionId: msg.SessionID,
		Type:      messageTypeToProto(msg.Type),
		Content:   msg.Content,
		Extra:     extraMapToStruct(msg.Extra),
	}
}

func messageTypeToProto(t MessageType) gogentv1.MessageType {
	switch t {
	case MessageTypeText:
		return gogentv1.MessageType_MESSAGE_TYPE_TEXT
	case MessageTypeImage:
		return gogentv1.MessageType_MESSAGE_TYPE_IMAGE
	case MessageTypeAudio:
		return gogentv1.MessageType_MESSAGE_TYPE_AUDIO
	case MessageTypeVideo:
		return gogentv1.MessageType_MESSAGE_TYPE_VIDEO
	case MessageTypeFile:
		return gogentv1.MessageType_MESSAGE_TYPE_FILE
	case MessageTypeSystem:
		return gogentv1.MessageType_MESSAGE_TYPE_SYSTEM
	default:
		return gogentv1.MessageType_MESSAGE_TYPE_UNSPECIFIED
	}
}

func extraMapToStruct(extra map[string]any) *structpb.Struct {
	if extra == nil {
		return nil
	}
	s, err := structpb.NewStruct(extra)
	if err != nil {
		return nil
	}
	return s
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func channelMessageFromProto(pb *gogentv1.ChannelMessage) Message {
	return Message{
		ID:        pb.Id,
		SessionID: pb.SessionId,
		Type:      messageTypeFromProto(pb.Type),
		Content:   pb.Content,
		Extra:     structToExtraMap(pb.Extra),
	}
}

func messageTypeFromProto(t gogentv1.MessageType) MessageType {
	switch t {
	case gogentv1.MessageType_MESSAGE_TYPE_TEXT:
		return MessageTypeText
	case gogentv1.MessageType_MESSAGE_TYPE_IMAGE:
		return MessageTypeImage
	case gogentv1.MessageType_MESSAGE_TYPE_AUDIO:
		return MessageTypeAudio
	case gogentv1.MessageType_MESSAGE_TYPE_VIDEO:
		return MessageTypeVideo
	case gogentv1.MessageType_MESSAGE_TYPE_FILE:
		return MessageTypeFile
	case gogentv1.MessageType_MESSAGE_TYPE_SYSTEM:
		return MessageTypeSystem
	default:
		return MessageType("")
	}
}

func structToExtraMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}
