package main

import (
	"sync"
	"time"

	"github.com/flynn/flynn/pkg/stream"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	"gopkg.in/inconshreveable/log15.v2"
)

type RouterDrainEvent struct {
	RouterID string
	Backend  *router.Backend
}

type Router struct {
	ID string

	events   chan *RouterDrainEvent
	client   routerc.Client
	logger   log15.Logger
	stop     chan struct{}
	stopOnce sync.Once
}

func NewRouter(id, addr string, events chan *RouterDrainEvent, logger log15.Logger) *Router {
	r := &Router{
		ID:     id,
		events: events,
		client: routerc.NewWithAddr(addr),
		logger: logger,
		stop:   make(chan struct{}),
	}
	go r.watchBackends()
	return r
}

func (r *Router) watchBackends() {
	log := r.logger.New("fn", "router.watchBackends", "router.id", r.ID)
	var events chan *router.StreamEvent
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting router event stream")
		events = make(chan *router.StreamEvent)
		opts := &router.StreamEventsOptions{
			EventTypes: []router.EventType{router.EventTypeDrain},
		}
		stream, err = r.client.StreamEvents(opts, events)
		if err != nil {
			log.Error("error connecting router event stream", "err", err)
		}
		return
	}

	// make initial connection
	for {
		if err := connect(); err == nil {
			defer stream.Close()
			break
		}
		select {
		case <-r.stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	for {
	eventLoop:
		for {
			select {
			case event, ok := <-events:
				if !ok {
					break eventLoop
				}
				r.events <- &RouterDrainEvent{
					RouterID: r.ID,
					Backend:  event.Backend,
				}
			case <-r.stop:
				return
			}
		}
		log.Warn("router event stream disconnected", "err", stream.Err())
		// keep trying to reconnect, unless we are told to stop
	retryLoop:
		for {
			select {
			case <-r.stop:
				return
			default:
			}

			if err := connect(); err == nil {
				break retryLoop
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (r *Router) Close() {
	r.stopOnce.Do(func() { close(r.stop) })
}
