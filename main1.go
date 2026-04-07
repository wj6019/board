package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Topic struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Replies   []Reply   `json:"replies"`
}

type Reply struct {
	ID        int       `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	Topics      []Topic `json:"topics"`
	NextTopicID int     `json:"next_topic_id"`
	NextReplyID int     `json:"next_reply_id"`
}

var (
	mu       sync.Mutex
	store    Store
	dataFile = "data.json"
	tpl      *template.Template
)

const indexHTML = `
{{define "index"}}
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body{
      max-width:760px;
      margin:24px auto;
      padding:0 14px;
      font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;
      background:#f7f7f7;
      color:#222;
      line-height:1.65;
    }
    a{color:#1565c0;text-decoration:none}
    a:hover{text-decoration:underline}
    .box{
      background:#fff;
      border:1px solid #e5e5e5;
      border-radius:8px;
      padding:16px;
      margin:14px 0;
    }
    input[type=text], textarea, input[type=password]{
      width:100%;
      box-sizing:border-box;
      padding:10px;
      border:1px solid #ccc;
      border-radius:6px;
      font-size:14px;
      background:#fff;
    }
    textarea{min-height:110px;resize:vertical}
    button{
      background:#111;
      color:#fff;
      border:none;
      border-radius:6px;
      padding:9px 14px;
      cursor:pointer;
    }
    button:hover{opacity:.92}
    .meta{
      color:#777;
      font-size:13px;
      margin-top:6px;
    }
    .topic-title{
      font-size:20px;
      font-weight:600;
      margin-bottom:4px;
    }
    .reply{
      border-top:1px dashed #e5e5e5;
      padding-top:12px;
      margin-top:12px;
    }
    .nav{margin-bottom:18px}
    .small{color:#888;font-size:13px}
    .danger{background:#b71c1c}
    form.inline{display:inline}
  </style>
</head>
<body>
  <div class="nav"><a href="/">首页</a></div>

  <h1>极简留言板</h1>
  <p class="small">匿名发帖，匿名回复，请文明发言。</p>

  <div class="box">
    <h2>新建主题</h2>
    <form method="post" action="/topic/new">
      <p><input type="text" name="author" maxlength="20" placeholder="昵称（可空，默认匿名）"></p>
      <p><input type="text" name="title" maxlength="80" placeholder="主题标题" required></p>
      <p><textarea name="content" maxlength="3000" placeholder="写点内容……" required></textarea></p>
      <p><button type="submit">发布主题</button></p>
    </form>
  </div>

  <div class="box">
    <h2>主题列表</h2>
    {{if .Topics}}
      {{range .Topics}}
        <div style="margin-bottom:18px;">
          <div class="topic-title"><a href="/topic?id={{.ID}}">{{.Title}}</a></div>
          <div>{{short .Content}}</div>
          <div class="meta">{{.Author}} · {{formatTime .CreatedAt}} · {{len .Replies}} 条回复</div>
        </div>
      {{end}}
    {{else}}
      <p>还没有主题。</p>
    {{end}}
  </div>
</body>
</html>
{{end}}
`

const topicHTML = `
{{define "topic"}}
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body{
      max-width:760px;
      margin:24px auto;
      padding:0 14px;
      font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;
      background:#f7f7f7;
      color:#222;
      line-height:1.65;
    }
    a{color:#1565c0;text-decoration:none}
    a:hover{text-decoration:underline}
    .box{
      background:#fff;
      border:1px solid #e5e5e5;
      border-radius:8px;
      padding:16px;
      margin:14px 0;
    }
    input[type=text], textarea, input[type=password]{
      width:100%;
      box-sizing:border-box;
      padding:10px;
      border:1px solid #ccc;
      border-radius:6px;
      font-size:14px;
      background:#fff;
    }
    textarea{min-height:110px;resize:vertical}
    button{
      background:#111;
      color:#fff;
      border:none;
      border-radius:6px;
      padding:9px 14px;
      cursor:pointer;
    }
    button:hover{opacity:.92}
    .meta{
      color:#777;
      font-size:13px;
      margin-top:6px;
    }
    .topic-title{
      font-size:20px;
      font-weight:600;
      margin-bottom:4px;
    }
    .reply{
      border-top:1px dashed #e5e5e5;
      padding-top:12px;
      margin-top:12px;
    }
    .nav{margin-bottom:18px}
    .small{color:#888;font-size:13px}
    .danger{background:#b71c1c}
    form.inline{display:inline}
  </style>
</head>
<body>
  <div class="nav"><a href="/">首页</a></div>

  <div class="box">
    <div class="topic-title">{{.Topic.Title}}</div>
    <div>{{nl2br .Topic.Content}}</div>
    <div class="meta">{{.Topic.Author}} · {{formatTime .Topic.CreatedAt}}</div>

    {{if .CanAdmin}}
    <div style="margin-top:12px;">
      <form class="inline" method="post" action="/topic/delete" onsubmit="return confirm('确定删除这个主题？');">
        <input type="hidden" name="topic_id" value="{{.Topic.ID}}">
        <input type="hidden" name="token" value="{{.Token}}">
        <button class="danger" type="submit">删除主题</button>
      </form>
    </div>
    {{end}}
  </div>

  <div class="box">
    <h2>回复</h2>
    {{if .Topic.Replies}}
      {{range .Topic.Replies}}
        <div class="reply">
          <div>{{nl2br .Content}}</div>
          <div class="meta">{{.Author}} · {{formatTime .CreatedAt}}</div>
          {{if $.CanAdmin}}
          <div style="margin-top:8px;">
            <form class="inline" method="post" action="/reply/delete" onsubmit="return confirm('确定删除这条回复？');">
              <input type="hidden" name="topic_id" value="{{$.Topic.ID}}">
              <input type="hidden" name="reply_id" value="{{.ID}}">
              <input type="hidden" name="token" value="{{$.Token}}">
              <button class="danger" type="submit">删除回复</button>
            </form>
          </div>
          {{end}}
        </div>
      {{end}}
    {{else}}
      <p>还没有回复。</p>
    {{end}}
  </div>

  <div class="box">
    <h2>发表回复</h2>
    <form method="post" action="/reply">
      <input type="hidden" name="topic_id" value="{{.Topic.ID}}">
      <p><input type="text" name="author" maxlength="20" placeholder="昵称（可空，默认匿名）"></p>
      <p><textarea name="content" maxlength="3000" placeholder="写下你的回复……" required></textarea></p>
      <p><button type="submit">提交回复</button></p>
    </form>
  </div>

  <div class="box">
    <h2>管理员</h2>
    <form method="get" action="/topic">
      <input type="hidden" name="id" value="{{.Topic.ID}}">
      <p><input type="password" name="token" placeholder="管理员口令"></p>
      <p><button type="submit">进入管理模式</button></p>
    </form>
  </div>
</body>
</html>
{{end}}
`

func main() {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04")
		},
		"short": func(s string) string {
			s = strings.TrimSpace(s)
			r := []rune(s)
			if len(r) > 100 {
				return string(r[:100]) + "..."
			}
			return s
		},
		"nl2br": func(s string) template.HTML {
			escaped := template.HTMLEscapeString(s)
			return template.HTML(strings.ReplaceAll(escaped, "\n", "<br>"))
		},
	}

	tpl = template.Must(template.New("root").Funcs(funcMap).Parse(indexHTML))
	template.Must(tpl.Parse(topicHTML))

	load()

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/topic", topicHandler)
	http.HandleFunc("/topic/new", newTopicHandler)
	http.HandleFunc("/reply", replyHandler)
	http.HandleFunc("/topic/delete", deleteTopicHandler)
	http.HandleFunc("/reply/delete", deleteReplyHandler)

	port := envOr("PORT", "15230")
	fmt.Println("Listening on :" + port)
	_ = http.ListenAndServe(":"+port, logMiddleware(http.DefaultServeMux))
}

func envOr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func adminOK(token string) bool {
	real := os.Getenv("ADMIN_TOKEN")
	if real == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(real)) == 1
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", time.Now().Format("2006-01-02 15:04:05"), r.Method, r.URL.String())
		next.ServeHTTP(w, r)
	})
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	mu.Lock()
	topics := make([]Topic, len(store.Topics))
	copy(topics, store.Topics)
	mu.Unlock()

	sort.Slice(topics, func(i, j int) bool {
		ti := topics[i].CreatedAt
		if len(topics[i].Replies) > 0 {
			ti = topics[i].Replies[len(topics[i].Replies)-1].CreatedAt
		}
		tj := topics[j].CreatedAt
		if len(topics[j].Replies) > 0 {
			tj = topics[j].Replies[len(topics[j].Replies)-1].CreatedAt
		}
		return ti.After(tj)
	})

	render(w, "index", struct {
		Title  string
		Topics []Topic
	}{
		Title:  "极简留言板",
		Topics: topics,
	})
}

func topicHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	token := r.URL.Query().Get("token")

	mu.Lock()
	defer mu.Unlock()

	for _, t := range store.Topics {
		if t.ID == id {
			render(w, "topic", struct {
				Title    string
				Topic    Topic
				CanAdmin bool
				Token    string
			}{
				Title:    t.Title,
				Topic:    t,
				CanAdmin: adminOK(token),
				Token:    token,
			})
			return
		}
	}

	http.NotFound(w, r)
}

func newTopicHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	author := cleanName(r.FormValue("author"))
	if author == "" {
		author = "匿名"
	}
	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))

	if title == "" || content == "" {
		http.Error(w, "标题和内容不能为空", http.StatusBadRequest)
		return
	}

	mu.Lock()
	topic := Topic{
		ID:        store.NextTopicID,
		Title:     limit(title, 80),
		Author:    limit(author, 20),
		Content:   limit(content, 3000),
		CreatedAt: time.Now(),
		Replies:   []Reply{},
	}
	store.NextTopicID++
	store.Topics = append(store.Topics, topic)
	saveLocked()
	mu.Unlock()

	http.Redirect(w, r, "/topic?id="+strconv.Itoa(topic.ID), http.StatusSeeOther)
}

func replyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	topicID, err := strconv.Atoi(r.FormValue("topic_id"))
	if err != nil || topicID <= 0 {
		http.Error(w, "invalid topic", http.StatusBadRequest)
		return
	}

	author := cleanName(r.FormValue("author"))
	if author == "" {
		author = "匿名"
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "回复不能为空", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range store.Topics {
		if store.Topics[i].ID == topicID {
			reply := Reply{
				ID:        store.NextReplyID,
				Author:    limit(author, 20),
				Content:   limit(content, 3000),
				CreatedAt: time.Now(),
			}
			store.NextReplyID++
			store.Topics[i].Replies = append(store.Topics[i].Replies, reply)
			saveLocked()
			http.Redirect(w, r, "/topic?id="+strconv.Itoa(topicID), http.StatusSeeOther)
			return
		}
	}

	http.Error(w, "topic not found", http.StatusNotFound)
}

func deleteTopicHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !adminOK(r.FormValue("token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	topicID, err := strconv.Atoi(r.FormValue("topic_id"))
	if err != nil || topicID <= 0 {
		http.Error(w, "bad topic id", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range store.Topics {
		if store.Topics[i].ID == topicID {
			store.Topics = append(store.Topics[:i], store.Topics[i+1:]...)
			saveLocked()
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	http.Error(w, "topic not found", http.StatusNotFound)
}

func deleteReplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !adminOK(r.FormValue("token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	topicID, err1 := strconv.Atoi(r.FormValue("topic_id"))
	replyID, err2 := strconv.Atoi(r.FormValue("reply_id"))
	if err1 != nil || err2 != nil || topicID <= 0 || replyID < 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range store.Topics {
		if store.Topics[i].ID == topicID {
			replies := store.Topics[i].Replies
			for j := range replies {
				if replies[j].ID == replyID {
					store.Topics[i].Replies = append(replies[:j], replies[j+1:]...)
					saveLocked()
					http.Redirect(w, r, "/topic?id="+strconv.Itoa(topicID)+"&token="+r.FormValue("token"), http.StatusSeeOther)
					return
				}
			}
		}
	}
	http.Error(w, "reply not found", http.StatusNotFound)
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func limit(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func load() {
	f, err := os.Open(dataFile)
	if err != nil {
		store = Store{Topics: []Topic{}, NextTopicID: 1, NextReplyID: 1}
		return
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		store = Store{Topics: []Topic{}, NextTopicID: 1, NextReplyID: 1}
		return
	}
	if err := json.Unmarshal(b, &store); err != nil {
		store = Store{Topics: []Topic{}, NextTopicID: 1, NextReplyID: 1}
		return
	}
	if store.NextTopicID <= 0 {
		store.NextTopicID = 1
	}
	if store.NextReplyID <= 0 {
		store.NextReplyID = 1
	}
	if store.Topics == nil {
		store.Topics = []Topic{}
	}
}

func saveLocked() {
	tmp := dataFile + ".tmp"
	b, _ := json.MarshalIndent(store, "", "  ")
	_ = os.WriteFile(tmp, b, 0644)
	_ = os.Rename(tmp, dataFile)
}
