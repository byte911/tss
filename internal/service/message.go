package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type Message struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type MessageService struct {
	js     nats.JetStreamContext
	logger *zap.Logger
}

func NewMessageService(js nats.JetStreamContext, logger *zap.Logger) *MessageService {
	return &MessageService{
		js:     js,
		logger: logger,
	}
}

func (s *MessageService) PublishMessage(ctx context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = s.js.Publish("messages", data)
	if err != nil {
		s.logger.Error("Failed to publish message",
			zap.String("message_id", msg.ID),
			zap.Error(err))
		return err
	}

	s.logger.Info("Message published successfully",
		zap.String("message_id", msg.ID))
	return nil
}

func (s *MessageService) SubscribeMessages(ctx context.Context, handler func(Message)) error {
	sub, err := s.js.Subscribe("messages", func(msg *nats.Msg) {
		var message Message
		if err := json.Unmarshal(msg.Data, &message); err != nil {
			s.logger.Error("Failed to unmarshal message",
				zap.Error(err))
			return
		}

		handler(message)
		msg.Ack()
	})

	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()

	return nil
}
