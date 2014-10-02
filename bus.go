package main

import (
	"code.google.com/p/go-uuid/uuid"
	"encoding/hex"
	"net/url"
	"strconv"
	"sync"
)

func MessageBus(sub <-chan MessageBusSubscription) {
	clientID := hex.EncodeToString(uuid.NewRandom())

	type messageBusSubscription struct {
		Call []MessageBusCallback
		Last int
	}
	subscriptions := make(map[string]*messageBusSubscription)
	var lock sync.Mutex

	go func() {
		for s := range sub {
			lock.Lock()
			if v, ok := subscriptions[s.Channel]; ok {
				v.Call = append(v.Call, s.Call)
			} else {
				subscriptions[s.Channel] = &messageBusSubscription{Call: []MessageBusCallback{s.Call}, Last: -1}
			}
			lock.Unlock()
		}
	}()

	for {
		TryTryAgain("message bus poll", func() {
			data := make(url.Values)

			lock.Lock()
			for c, s := range subscriptions {
				data.Set(c, strconv.Itoa(s.Last))
			}
			lock.Unlock()

			j, err := PostArray("/message-bus/"+clientID+"/poll?", data)
			if err != nil {
				panic(err)
			}
			for _, m := range j {
				msg := m.(map[string]interface{})
				c := msg["channel"].(string)
				msgdata := msg["data"].(map[string]interface{})
				lock.Lock()
				if c == "/__status" {
					for ch, id := range msgdata {
						if v, ok := subscriptions[ch]; ok {
							v.Last = int(id.(float64))
						}
					}
				} else {
					s := subscriptions[c]
					if s != nil {
						for _, call := range s.Call {
							go call(msgdata)
						}
						s.Last = int(msg["message_id"].(float64))
					}
				}
				lock.Unlock()
			}
		})
	}
}

type MessageBusSubscription struct {
	Channel string
	Call    MessageBusCallback
}

type MessageBusCallback func(map[string]interface{})
