package notification

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	EventTypeCreated = "notification.created"

	CategoryTransactional = "transactional"
	CategorySocial        = "social"
	CategoryMarketing     = "marketing"

	ChannelEmail = "email"
	ChannelSMS   = "sms"
	ChannelPush  = "push"
)

type CreatedEvent struct {
	NotificationType string          `json:"notification_type"`
	Category         string          `json:"category"`
	RecipientID      string          `json:"recipient_id"`
	Targets          []Target        `json:"targets"`
	Payload          json.RawMessage `json:"payload"`
}

type Target struct {
	Channel     string `json:"channel"`
	Destination string `json:"destination"`
}

func (e CreatedEvent) Validate() error {
	if e.NotificationType == "" {
		return errors.New("notification type is required")
	}

	if e.Category == "" {
		return errors.New("category is required")
	}

	if e.RecipientID == "" {
		return errors.New("recipient id is required")
	}

	if len(e.Targets) == 0 {
		return errors.New("at least one target is required")
	}

	if len(e.Payload) == 0 {
		return errors.New("payload is required")
	}

	if !json.Valid(e.Payload) {
		return errors.New("payload must be valid JSON")
	}

	seenChannels := make(map[string]struct{}, len(e.Targets))

	for i, target := range e.Targets {
		if err := target.Validate(); err != nil {
			return fmt.Errorf("target[%d]: %w", i, err)
		}

		if _, exists := seenChannels[target.Channel]; exists {
			return fmt.Errorf(
				"target[%d]: duplicate channel %q",
				i,
				target.Channel,
			)
		}

		seenChannels[target.Channel] = struct{}{}
	}

	return nil
}

func (t Target) Validate() error {
	if t.Channel == "" {
		return errors.New("channel is required")
	}

	switch t.Channel {
	case ChannelEmail, ChannelSMS, ChannelPush:
	default:
		return fmt.Errorf("unsupported channel %q", t.Channel)
	}

	if t.Destination == "" {
		return errors.New("destination is required")
	}

	return nil
}
