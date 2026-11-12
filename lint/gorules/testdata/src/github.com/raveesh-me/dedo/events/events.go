// Package events stands in for dedo's.
package events

import "context"

const ReasonNotPublished = "EVENT_NOT_PUBLISHED"

type Event struct{}

type Option func()

type Publisher struct{}

func (p *Publisher) Publish(ctx context.Context, eventType, subject string, opts ...Option) (Event, error) {
	return Event{}, nil
}
