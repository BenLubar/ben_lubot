package main

import (
	"code.google.com/p/go.net/publicsuffix"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var Client http.Client
var MyID int
var Subscribe = make(chan MessageBusSubscription)

func init() {
	blt := &ben_lubot_transport{base: http.DefaultTransport}
	Client.Transport = blt
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		panic(err)
	}
	Client.Jar = jar

	v, err := GetJSON("/session/csrf")
	if err != nil {
		panic(err)
	}
	blt.csrf = v["csrf"].(string)
}

type ben_lubot_transport struct {
	base http.RoundTripper
	csrf string
	wait sync.Mutex
}

func (blt *ben_lubot_transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "POST" {
		q := req.URL.Query()
		q.Set("_", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-SILENCE-LOGGER", "true")
	if blt.csrf != "" {
		req.Header.Set("X-CSRF-Token", blt.csrf)
	}

	blt.wait.Lock()
	time.Sleep(time.Second)
	blt.wait.Unlock()

	start := time.Now()
	resp, err := blt.base.RoundTrip(req)
	log.Println(req.Method, req.URL.RequestURI(), time.Since(start))

	return resp, err
}

func GetJSON(path string) (v map[string]interface{}, err error) {
	resp, err := Client.Get(Host + path)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&v)
	if err != nil {
		return
	}

	return
}

func PostJSON(path string, data url.Values) (v map[string]interface{}, err error) {
	resp, err := Client.PostForm(Host+path, data)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&v)
	if err != nil {
		return
	}

	return
}

func PostArray(path string, data url.Values) (v []interface{}, err error) {
	resp, err := Client.PostForm(Host+path, data)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&v)
	if err != nil {
		return
	}

	return
}

func PostDiscard(path string, data url.Values) (err error) {
	resp, err := Client.PostForm(Host+path, data)
	if err != nil {
		return
	}

	err = resp.Body.Close()
	if err != nil {
		return
	}

	return
}

func ReadTopic(id int) (posts int) {
	s_id := strconv.Itoa(id)

	add, last_read := "", 0
	for {
		topic, err := GetJSON("/t/" + s_id + ".json?track_visit=true" + add)
		if err != nil {
			panic(err)
		}

		lrf, _ := topic["last_read_post_number"].(float64)
		lr := int(lrf)
		if lr >= int(topic["posts_count"].(float64)) {
			return
		}
		if lr > last_read {
			last_read = lr
		}

		n := strconv.Itoa(1000*60*3 + rand.Intn(100))
		v := url.Values{
			"topic_time": {n},
			"topic_id":   {s_id},
		}
		unread := false
		for _, p := range topic["post_stream"].(map[string]interface{})["posts"].([]interface{}) {
			read, _ := p.(map[string]interface{})["read"].(bool)
			if !read {
				pid := int(p.(map[string]interface{})["post_number"].(float64))
				v["timings["+strconv.Itoa(pid)+"]"] = []string{n}

				posts++
				unread = true

				if pid > last_read {
					last_read = pid
				}
			}
		}

		if unread {
			err = PostDiscard("/topics/timings", v)
			if err != nil {
				panic(err)
			}
		}

		add = "&post_number=" + strconv.Itoa(last_read)
	}
}

func getTopics(url string) (topics []int) {
	for {
		j, err := GetJSON(url)
		if err != nil {
			panic(err)
		}

		tl := j["topic_list"].(map[string]interface{})

		for _, t := range tl["topics"].([]interface{}) {
			topics = append(topics, int(t.(map[string]interface{})["id"].(float64)))
		}

		var ok bool
		url, ok = tl["more_topics_url"].(string)
		if !ok {
			return
		}
	}
}

func GetNewTopics() []int    { return getTopics("/new.json") }
func GetUnreadTopics() []int { return getTopics("/unread.json") }
func GetUnreadPrivateMessages() []int {
	return getTopics("/topics/private-messages-unread/" + Username + ".json")
}

func Backfill(topics []int) {
	log.Println("reading", len(topics), "topics")

	posts := 0

	for i, t := range topics {
		p := ReadTopic(t)
		posts += p
		log.Printf("reading topics: %d/%d [%d/%d posts]", i+1, len(topics), p, posts)
	}

	log.Println("read", posts, "posts across", len(topics), "topics")
}

func Authenticate() {
	v, err := PostJSON("/session", url.Values{"login": {Username}, "password": {Password}})
	if err != nil {
		panic(err)
	}

	MyID = int(v["user"].(map[string]interface{})["id"].(float64))
}

func main() {
	Authenticate()

	go MessageBus(Subscribe)

	go func() {
		for {
			Backfill(GetNewTopics())
			Backfill(GetUnreadTopics())
			Backfill(GetUnreadPrivateMessages())

			const naptime = time.Hour
			log.Println("going to sleep until", time.Now().Add(naptime))
			time.Sleep(naptime)
		}
	}()

	go func() {
		sub := func(channel string) {
			Subscribe <- MessageBusSubscription{channel, func(data map[string]interface{}) {
				log.Println("MESSAGE_BUS", channel, data)
				if tid, ok := data["topic_id"]; ok {
					id := int(tid.(float64))
					log.Println("read", ReadTopic(id), "posts from topic", id, "(notifications"+channel+")")
				}
			}}
		}
		sub("/latest")
		sub("/new")
		sub("/unread/" + strconv.Itoa(MyID))
		sub("/notification/" + strconv.Itoa(MyID))
	}()

	select {}
}
