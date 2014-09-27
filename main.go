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

func ReadTopic(id int) {
	s_id := strconv.Itoa(id)

	add, last_read := "", 0
	for {
		topic, err := GetJSON("/t/" + s_id + ".json" + add)
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

		add = "?post_number=" + strconv.Itoa(last_read)
	}
}

func BackfillLatest() {
	url := "/latest.json"
	for {
		latest, err := GetJSON(url)
		if err != nil {
			panic(err)
		}

		tl := latest["topic_list"].(map[string]interface{})

		for _, t := range tl["topics"].([]interface{}) {
			ReadTopic(int(t.(map[string]interface{})["id"].(float64)))
		}

		var ok bool
		url, ok = tl["more_topics_url"].(string)
		if !ok {
			return
		}
	}
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
	BackfillLatest()
}
