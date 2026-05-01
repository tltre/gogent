package channel

import "context"

type MessageType string

const (
	MessageTypeText   MessageType = "text"
	MessageTypeImage  MessageType = "image"
	MessageTypeAudio  MessageType = "audio"
	MessageTypeVideo  MessageType = "video"
	MessageTypeFile   MessageType = "file"
	MessageTypeSystem MessageType = "system"
)

type Message struct {
	ID        string
	SessionID string
	Type      MessageType
	Content   string
	Extra     map[string]any
}

type IChannel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Receive(ctx context.Context) (<-chan Message, error)
	Send(ctx context.Context, msg Message) error
}

//type MessageHandler interface {
//	Handle(ctx context.Context, msg Message) (Message, error)
//}
