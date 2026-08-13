package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	gsm "github.com/bradleypeabody/gorilla-sessions-memcache"
	"github.com/catatsuy/private-isu/webapp/golang/isuutil"
	"github.com/go-chi/chi/v5"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
)

var (
	db                       *sqlx.DB
	store                    *gsm.MemcacheStore
	indexCache               indexPostsCache
	postsPageCache           postsPageCacheStore
	userCache                userCacheStore
	postCache                postCacheStore
	commentCountCache        commentCountCacheStore
	userCommentCountCache    userCommentCountCacheStore
	loginCache               loginCacheStore
	profileCache             accountProfileCache
	commentsCache            commentsCacheStore
	indexPostsHTMLCacheStore indexPostsHTMLCache
	postHTMLCacheStore       postHTMLCache
	templates                struct {
		index    *template.Template
		login    *template.Template
		register *template.Template
		user     *template.Template
		posts    *template.Template
		post     *template.Template
		postID   *template.Template
		banned   *template.Template
	}
)

const (
	postsPerPage  = 20
	ISO8601Format = "2006-01-02T15:04:05-07:00"
	UploadLimit   = 10 * 1024 * 1024 // 10mb
	imageDir      = "../public/image"
)

type User struct {
	ID          int       `db:"id"`
	AccountName string    `db:"account_name"`
	Passhash    string    `db:"passhash"`
	Authority   int       `db:"authority"`
	DelFlg      int       `db:"del_flg"`
	CreatedAt   time.Time `db:"created_at"`
}

type Post struct {
	ID           int       `db:"id"`
	UserID       int       `db:"user_id"`
	Imgdata      []byte    `db:"imgdata"`
	Body         string    `db:"body"`
	Mime         string    `db:"mime"`
	CreatedAt    time.Time `db:"created_at"`
	CommentCount int
	Comments     []Comment
	User         User
	CSRFToken    string
}

type Comment struct {
	ID        int       `db:"id"`
	PostID    int       `db:"post_id"`
	UserID    int       `db:"user_id"`
	Comment   string    `db:"comment"`
	CreatedAt time.Time `db:"created_at"`
	User      User
}

var memcacheClient *memcache.Client

func init() {
	memdAddr := os.Getenv("ISUCONP_MEMCACHED_ADDRESS")
	if memdAddr == "" {
		memdAddr = "localhost:11211"
	}
	memcacheClient = memcache.New(memdAddr)
	store = gsm.NewMemcacheStore(memcacheClient, "iscogram_", []byte("sendagaya"))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func dbInitialize(ctx context.Context) {
	sqls := []string{
		"DELETE FROM users WHERE id > 1000",
		"DELETE FROM posts WHERE id > 10000",
		"DELETE FROM comments WHERE id > 100000",
		"UPDATE users SET del_flg = 0",
		"UPDATE users SET del_flg = 1 WHERE id % 50 = 0",
	}

	for _, sql := range sqls {
		db.ExecContext(ctx, sql)
	}
	indexCache.invalidate()
	postsPageCache.invalidate()
	userCache.invalidate()
	postCache.invalidate()
	commentCountCache.invalidate()
	userCommentCountCache.invalidate()
	loginCache.invalidate()
	profileCache.invalidateAll()
	commentsCache.invalidateAll()
	indexPostsHTMLCacheStore.invalidate()
	postHTMLCacheStore.invalidate()

	// コメント一覧の絞り込みと created_at 順の取得を同じインデックスで処理できるようにする。
	// initialize はベンチマークごとに呼ばれるため、既存の場合はエラーにしない。
	if err := isuutil.CreateIndexIfNotExists(db, "CREATE INDEX idx_comments_post_id_created_at ON comments (post_id, created_at)"); err != nil {
		log.Printf("failed to create comments index: %v", err)
	}
	if err := isuutil.CreateIndexIfNotExists(db, "CREATE INDEX idx_posts_created_at ON posts (created_at DESC)"); err != nil {
		log.Printf("failed to create posts created_at index: %v", err)
	}
	if err := isuutil.CreateIndexIfNotExists(db, "CREATE INDEX idx_posts_user_id_created_at ON posts (user_id, created_at DESC)"); err != nil {
		log.Printf("failed to create posts user_id/created_at index: %v", err)
	}
	if err := isuutil.CreateIndexIfNotExists(db, "CREATE INDEX idx_comments_user_id ON comments (user_id)"); err != nil {
		log.Printf("failed to create comments user_id index: %v", err)
	}
}

func tryLogin(ctx context.Context, accountName, password string) *User {
	u, err := loginCache.load(ctx, db, accountName)
	if err != nil {
		return nil
	}
	if u == nil {
		return nil
	}

	if calculatePasshash(ctx, u.AccountName, password) == u.Passhash {
		return u
	} else {
		return nil
	}
}

func validateUser(accountName, password string) bool {
	return regexp.MustCompile(`\A[0-9a-zA-Z_]{3,}\z`).MatchString(accountName) &&
		regexp.MustCompile(`\A[0-9a-zA-Z_]{6,}\z`).MatchString(password)
}

func digest(ctx context.Context, src string) string {
	_ = ctx
	sum := sha512.Sum512([]byte(src))
	return hex.EncodeToString(sum[:])
}

func calculateSalt(ctx context.Context, accountName string) string {
	return digest(ctx, accountName)
}

func calculatePasshash(ctx context.Context, accountName, password string) string {
	return digest(ctx, password+":"+calculateSalt(ctx, accountName))
}

func getSession(r *http.Request) *sessions.Session {
	session, _ := store.Get(r, "isuconp-go.session")

	return session
}

func getSessionUser(r *http.Request) User {
	ctx := r.Context()
	session := getSession(r)
	uid, ok := session.Values["user_id"]
	if !ok || uid == nil {
		return User{}
	}

	var userID int
	switch value := uid.(type) {
	case int:
		userID = value
	case int64:
		userID = int(value)
	default:
		return User{}
	}

	users, err := userCache.load(ctx, db, []int{userID})
	if err != nil {
		return User{}
	}
	return users[userID]
}

func getFlash(w http.ResponseWriter, r *http.Request, key string) string {
	session := getSession(r)
	value, ok := session.Values[key]

	if !ok || value == nil {
		return ""
	} else {
		delete(session.Values, key)
		session.Save(r, w)
		return value.(string)
	}
}

func makePosts(ctx context.Context, results []Post, csrfToken string, allComments bool) ([]Post, error) {
	return loadPosts(ctx, db, results, csrfToken, allComments)
}

func imageURL(p Post) string {
	ext := ""
	if p.Mime == "image/jpeg" {
		ext = ".jpg"
	} else if p.Mime == "image/png" {
		ext = ".png"
	} else if p.Mime == "image/gif" {
		ext = ".gif"
	}

	return "/image/" + strconv.Itoa(p.ID) + ext
}

func isLogin(u User) bool {
	return u.ID != 0
}

func getCSRFToken(r *http.Request) string {
	session := getSession(r)
	csrfToken, ok := session.Values["csrf_token"]
	if !ok {
		return ""
	}
	return csrfToken.(string)
}

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := crand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
}

func getTemplPath(filename string) string {
	return path.Join("templates", filename)
}

func loadTemplate(name string, files ...string) *template.Template {
	fmap := template.FuncMap{
		"imageURL": imageURL,
	}

	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = getTemplPath(file)
	}
	return template.Must(template.New(name).Funcs(fmap).ParseFiles(paths...))
}

func loadTemplates() {
	templates.index = loadTemplate("layout.html", "layout.html", "index.html", "posts.html", "post.html")
	templates.login = loadTemplate("layout.html", "layout.html", "login.html")
	templates.register = loadTemplate("layout.html", "layout.html", "register.html")
	templates.user = loadTemplate("layout.html", "layout.html", "user.html", "posts.html", "post.html")
	templates.posts = loadTemplate("posts.html", "posts.html", "post.html")
	templates.post = loadTemplate("post.html", "post.html")
	templates.postID = loadTemplate("layout.html", "layout.html", "post_id.html", "post.html")
	templates.banned = loadTemplate("layout.html", "layout.html", "banned.html")
}

func getInitialize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbInitialize(ctx)
	if err := cleanupGeneratedImages(imageDir, 10000); err != nil {
		log.Printf("failed to clean up images: %v", err)
		http.Error(w, "failed to clean up images", http.StatusInternalServerError)
		return
	}
	if err := isuutil.KickPproteinCollect(); err != nil {
		log.Printf("pprotein collect was not started: %v", err)
	}
	w.WriteHeader(http.StatusOK)
}

func getExportImages(w http.ResponseWriter, r *http.Request) {
	if err := exportImages(r.Context(), db, imageDir); err != nil {
		log.Printf("failed to export images: %v", err)
		http.Error(w, "failed to export images", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func getLogin(w http.ResponseWriter, r *http.Request) {
	me := getSessionUser(r)

	if isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if err := templates.login.Execute(w, struct {
		Me    User
		Flash string
	}{me, getFlash(w, r, "notice")}); err != nil {
		log.Print(err)
	}
}

func postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	u := tryLogin(ctx, r.FormValue("account_name"), r.FormValue("password"))

	if u != nil {
		session := getSession(r)
		session.Values["user_id"] = u.ID
		session.Values["csrf_token"] = secureRandomStr(16)
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		session := getSession(r)
		session.Values["notice"] = "アカウント名かパスワードが間違っています"
		session.Save(r, w)

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func getRegister(w http.ResponseWriter, r *http.Request) {
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if err := templates.register.Execute(w, struct {
		Me    User
		Flash string
	}{User{}, getFlash(w, r, "notice")}); err != nil {
		log.Print(err)
	}
}

func postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	accountName, password := r.FormValue("account_name"), r.FormValue("password")

	validated := validateUser(accountName, password)
	if !validated {
		session := getSession(r)
		session.Values["notice"] = "アカウント名は3文字以上、パスワードは6文字以上である必要があります"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	exists := 0
	// ユーザーが存在しない場合はエラーになるのでエラーチェックはしない
	db.GetContext(ctx, &exists, "SELECT 1 FROM users WHERE `account_name` = ?", accountName)

	if exists == 1 {
		session := getSession(r)
		session.Values["notice"] = "アカウント名がすでに使われています"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	query := "INSERT INTO `users` (`account_name`, `passhash`) VALUES (?,?)"
	result, err := db.ExecContext(ctx, query, accountName, calculatePasshash(ctx, accountName, password))
	if err != nil {
		log.Print(err)
		return
	}

	session := getSession(r)
	uid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}
	loginCache.invalidate(accountName)
	session.Values["user_id"] = uid
	session.Values["csrf_token"] = secureRandomStr(16)
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getLogout(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	delete(session.Values, "user_id")
	session.Options = &sessions.Options{MaxAge: -1}
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)

	results, err := indexCache.load(ctx, db)
	if err != nil {
		log.Print(err)
		return
	}

	postsHTML, ok := indexPostsHTMLCacheStore.load()
	if !ok {
		postsHTML, err = renderPostListHTML(ctx, results, indexCSRFPlaceholder, false)
		if err != nil {
			log.Print(err)
			return
		}
		indexPostsHTMLCacheStore.store(postsHTML)
	}
	csrfToken := getCSRFToken(r)
	postsHTML = template.HTML(strings.ReplaceAll(string(postsHTML), indexCSRFPlaceholder, template.HTMLEscapeString(csrfToken)))
	if err := renderIndexPage(w, postsHTML, me, csrfToken, getFlash(w, r, "notice")); err != nil {
		log.Print(err)
	}
}

// renderIndexPage writes the fixed index shell directly. The post list is
// already rendered and cached separately, so executing the full layout and
// nested templates for every request only adds reflection and traversal cost.
func renderIndexPage(w io.Writer, postsHTML template.HTML, me User, csrfToken, flash string) error {
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <title>Iscogram</title>
    <link href="/css/style.css" media="screen" rel="stylesheet" type="text/css">
  </head>
  <body>
    <div class="container">
      <div class="header">
        <div class="isu-title">
          <h1><a href="/">Iscogram</a></h1>
        </div>
        <div class="isu-header-menu">
`)
	if me.ID == 0 {
		buf.WriteString(`          <div><a href="/login">ログイン</a></div>
`)
	} else {
		accountName := template.HTMLEscapeString(me.AccountName)
		buf.WriteString(`          <div><a href="/@`)
		buf.WriteString(accountName)
		buf.WriteString(`"><span class="isu-account-name">`)
		buf.WriteString(accountName)
		buf.WriteString(`</span>さん</a></div>
`)
		if me.Authority == 1 {
			buf.WriteString(`          <div><a href="/admin/banned">管理者用ページ</a></div>
`)
		}
		buf.WriteString(`          <div><a href="/logout">ログアウト</a></div>
`)
	}
	buf.WriteString(`        </div>
      </div>
      <div class="isu-submit">
        <form method="post" action="/" enctype="multipart/form-data">
          <div class="isu-form">
            <input type="file" name="file" value="file">
          </div>
          <div class="isu-form">
            <textarea name="body"></textarea>
          </div>
          <div class="form-submit">
            <input type="hidden" name="csrf_token" value="`)
	buf.WriteString(template.HTMLEscapeString(csrfToken))
	buf.WriteString(`">
            <input type="submit" name="submit" value="submit">
          </div>
`)
	if flash != "" {
		buf.WriteString(`          <div id="notice-message" class="alert alert-danger">
            `)
		buf.WriteString(template.HTMLEscapeString(flash))
		buf.WriteString(`
          </div>
`)
	}
	buf.WriteString(`        </form>
      </div>
`)
	buf.WriteString(string(postsHTML))
	buf.WriteString(`
      <div id="isu-post-more">
        <button id="isu-post-more-btn">もっと見る</button>
        <img class="isu-loading-icon" src="/img/ajax-loader.gif">
      </div>
    </div>
    <script src="/js/timeago.min.js"></script>
    <script src="/js/main.js"></script>
  </body>
</html>
`)
	_, err := w.Write(buf.Bytes())
	return err
}

// renderPostIDPage writes the fixed post detail shell directly. The post
// fragment is already cached, so executing layout.html for every request only
// adds template reflection and traversal overhead.
func renderPostIDPage(w io.Writer, postHTML template.HTML, me User) error {
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <title>Iscogram</title>
    <link href="/css/style.css" media="screen" rel="stylesheet" type="text/css">
  </head>
  <body>
    <div class="container">
      <div class="header">
        <div class="isu-title">
          <h1><a href="/">Iscogram</a></h1>
        </div>
        <div class="isu-header-menu">
`)
	if me.ID == 0 {
		buf.WriteString(`          <div><a href="/login">ログイン</a></div>
`)
	} else {
		accountName := template.HTMLEscapeString(me.AccountName)
		buf.WriteString(`          <div><a href="/@`)
		buf.WriteString(accountName)
		buf.WriteString(`"><span class="isu-account-name">`)
		buf.WriteString(accountName)
		buf.WriteString(`</span>さん</a></div>
`)
		if me.Authority == 1 {
			buf.WriteString(`          <div><a href="/admin/banned">管理者用ページ</a></div>
`)
		}
		buf.WriteString(`          <div><a href="/logout">ログアウト</a></div>
`)
	}
	buf.WriteString(`        </div>
      </div>

`)
	buf.WriteString(string(postHTML))
	buf.WriteString(`
    </div>
    <script src="/js/timeago.min.js"></script>
    <script src="/js/main.js"></script>
  </body>
</html>
`)
	_, err := w.Write(buf.Bytes())
	return err
}

// renderPostListHTML renders only posts that are not already present in the
// per-post cache. The list cache is invalidated when any visible post data
// changes, but most posts remain unchanged and can reuse their fragments.
func renderPostListHTML(ctx context.Context, results []Post, csrfToken string, allComments bool) (template.HTML, error) {
	postsHTML := make([]template.HTML, len(results))
	missingResults := make([]Post, 0, len(results))
	missingIndexes := make(map[int][]int)
	for i, result := range results {
		if html, ok := postHTMLCacheStore.load(result.ID, allComments); ok {
			postsHTML[i] = html
			continue
		}
		missingResults = append(missingResults, result)
		missingIndexes[result.ID] = append(missingIndexes[result.ID], i)
	}

	if len(missingResults) > 0 {
		posts, err := makePosts(ctx, missingResults, csrfToken, allComments)
		if err != nil {
			return "", err
		}
		for _, post := range posts {
			var buf bytes.Buffer
			if err := templates.post.Execute(&buf, post); err != nil {
				return "", err
			}
			html := template.HTML(buf.String())
			postHTMLCacheStore.store(post.ID, allComments, html)
			for _, i := range missingIndexes[post.ID] {
				postsHTML[i] = html
			}
		}
	}

	var buf bytes.Buffer
	buf.WriteString("<div class=\"isu-posts\">\n")
	for _, html := range postsHTML {
		buf.WriteByte('\n')
		buf.WriteString(string(html))
	}
	buf.WriteString("\n</div>")
	return template.HTML(buf.String()), nil
}

func getAccountName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountName := r.PathValue("accountName")
	user, err := profileCache.loadUser(ctx, db, accountName)
	if err != nil {
		log.Print(err)
		return
	}

	if user.ID == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	results, err := profileCache.loadPosts(ctx, db, user.ID)
	if err != nil {
		log.Print(err)
		return
	}

	postsHTML, err := renderPostListHTML(ctx, results, indexCSRFPlaceholder, false)
	if err != nil {
		log.Print(err)
		return
	}
	postsHTML = template.HTML(strings.ReplaceAll(string(postsHTML), indexCSRFPlaceholder, template.HTMLEscapeString(getCSRFToken(r))))

	commentCount, err := userCommentCountCache.load(ctx, db, user.ID)
	if err != nil {
		log.Print(err)
		return
	}

	postIDs := make([]int, 0, len(results))
	for _, post := range results {
		postIDs = append(postIDs, post.ID)
	}
	postCount := len(postIDs)

	commentCounts, err := loadCommentCounts(ctx, db, postIDs)
	if err != nil {
		log.Print(err)
		return
	}
	commentedCount := 0
	for _, count := range commentCounts {
		commentedCount += count
	}

	me := getSessionUser(r)

	if err := templates.user.Execute(w, struct {
		PostsHTML      template.HTML
		User           User
		PostCount      int
		CommentCount   int
		CommentedCount int
		Me             User
	}{postsHTML, user, postCount, commentCount, commentedCount, me}); err != nil {
		log.Print(err)
	}
}

func getPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Print(err)
		return
	}
	maxCreatedAt := m.Get("max_created_at")
	if maxCreatedAt == "" {
		return
	}

	t, err := time.Parse(ISO8601Format, maxCreatedAt)
	if err != nil {
		log.Print(err)
		return
	}

	results, err := postsPageCache.load(ctx, db, t.Format(ISO8601Format))
	if err != nil {
		log.Print(err)
		return
	}

	csrfToken := getCSRFToken(r)
	postsHTML := make([]template.HTML, len(results))
	missingResults := make([]Post, 0, len(results))
	missingIndexes := make(map[int][]int)
	for i, result := range results {
		if html, ok := postHTMLCacheStore.load(result.ID, false); ok {
			postsHTML[i] = html
			continue
		}
		missingResults = append(missingResults, result)
		missingIndexes[result.ID] = append(missingIndexes[result.ID], i)
	}

	if len(missingResults) > 0 {
		posts, err := makePosts(ctx, missingResults, indexCSRFPlaceholder, false)
		if err != nil {
			log.Print(err)
			return
		}
		for _, post := range posts {
			var buf bytes.Buffer
			if err := templates.post.Execute(&buf, post); err != nil {
				log.Print(err)
				return
			}
			html := template.HTML(buf.String())
			postHTMLCacheStore.store(post.ID, false, html)
			for _, i := range missingIndexes[post.ID] {
				postsHTML[i] = html
			}
		}
	}

	if len(results) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	for _, html := range postsHTML {
		if html == "" {
			continue
		}
		rendered := strings.ReplaceAll(string(html), indexCSRFPlaceholder, template.HTMLEscapeString(csrfToken))
		if _, err := io.WriteString(w, rendered); err != nil {
			log.Print(err)
			return
		}
	}
}

func getPostsID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pidStr := r.PathValue("id")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	postHTML, ok := postHTMLCacheStore.load(pid, true)
	if !ok {
		results, err := postCache.load(ctx, db, pid)
		if err != nil {
			log.Print(err)
			return
		}

		posts, err := makePosts(ctx, results, indexCSRFPlaceholder, true)
		if err != nil {
			log.Print(err)
			return
		}

		if len(posts) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var buf bytes.Buffer
		if err := templates.post.Execute(&buf, posts[0]); err != nil {
			log.Print(err)
			return
		}
		postHTML = template.HTML(buf.String())
		postHTMLCacheStore.store(pid, true, postHTML)
	}
	postHTML = template.HTML(strings.ReplaceAll(string(postHTML), indexCSRFPlaceholder, template.HTMLEscapeString(getCSRFToken(r))))

	me := getSessionUser(r)

	if err := renderPostIDPage(w, postHTML, me); err != nil {
		log.Print(err)
	}
}

func postIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	post, err := parsePostMultipart(r)
	if err != nil {
		session := getSession(r)
		session.Values["notice"] = multipartErrorMessage(err)
		session.Save(r, w)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if post.csrf != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	if post.fileData == nil {
		session := getSession(r)
		session.Values["notice"] = "画像が必須です"
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	body := post.body
	mime := post.mime
	createdAt := time.Now()
	// 画像本体はファイルとして保存する。imgdataは既存スキーマ互換のため
	// 空のBLOBを入れるが、リクエスト経路では参照しない。
	query := "INSERT INTO `posts` (`user_id`, `mime`, `imgdata`, `body`, `created_at`) VALUES (?,?,?,?,?)"
	result, err := db.ExecContext(
		ctx,
		query,
		me.ID,
		mime,
		[]byte{},
		body,
		createdAt,
	)
	if err != nil {
		log.Print(err)
		return
	}

	pid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}
	if err := saveImageFile(imageDir, pid, mime, post.fileData); err != nil {
		log.Printf("failed to save image for post %d: %v", pid, err)
		if _, deleteErr := db.ExecContext(ctx, "DELETE FROM `posts` WHERE `id` = ?", pid); deleteErr != nil {
			log.Printf("failed to rollback post %d after image save error: %v", pid, deleteErr)
		}
		return
	}
	indexCache.invalidate()
	postsPageCache.invalidate()
	indexPostsHTMLCacheStore.invalidate()
	profileCache.appendPost(Post{
		ID:        int(pid),
		UserID:    me.ID,
		Body:      body,
		Mime:      mime,
		CreatedAt: createdAt,
	})

	http.Redirect(w, r, "/posts/"+strconv.FormatInt(pid, 10), http.StatusFound)
}

func getImage(w http.ResponseWriter, r *http.Request) {
	// /image/ はNginxのtry_filesで静的配信するため、Go側はフォールバックとして
	// DBや画像データを参照せず、到達した場合は404だけを返す。
	http.NotFound(w, r)
}

func postComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	postID, err := strconv.Atoi(r.FormValue("post_id"))
	if err != nil {
		log.Print("post_idは整数のみです")
		return
	}

	query := "INSERT INTO `comments` (`post_id`, `user_id`, `comment`) VALUES (?,?,?)"
	_, err = db.ExecContext(ctx, query, postID, me.ID, r.FormValue("comment"))
	if err != nil {
		log.Print(err)
		return
	}
	commentCountCache.invalidate(postID)
	commentsCache.invalidate(postID)
	indexPostsHTMLCacheStore.invalidate()
	postHTMLCacheStore.invalidate(postID)
	userCommentCountCache.invalidate(me.ID)

	http.Redirect(w, r, fmt.Sprintf("/posts/%d", postID), http.StatusFound)
}

func getAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	users := []User{}
	err := db.SelectContext(ctx, &users, "SELECT * FROM `users` WHERE `authority` = 0 AND `del_flg` = 0 ORDER BY `created_at` DESC")
	if err != nil {
		log.Print(err)
		return
	}

	if err := templates.banned.Execute(w, struct {
		Users     []User
		Me        User
		CSRFToken string
	}{users, me, getCSRFToken(r)}); err != nil {
		log.Print(err)
	}
}

func postAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	query := "UPDATE `users` SET `del_flg` = ? WHERE `id` = ?"

	err := r.ParseForm()
	if err != nil {
		log.Print(err)
		return
	}

	for _, id := range r.Form["uid[]"] {
		if _, err := db.ExecContext(ctx, query, 1, id); err == nil {
			uid, parseErr := strconv.Atoi(id)
			if parseErr == nil {
				userCache.invalidate(uid)
				profileCache.invalidateUser(uid)
			}
		}
	}
	indexCache.invalidate()
	postsPageCache.invalidate()
	indexPostsHTMLCacheStore.invalidate()
	postHTMLCacheStore.invalidate()
	loginCache.invalidate()

	http.Redirect(w, r, "/admin/banned", http.StatusFound)
}

func main() {
	host := os.Getenv("ISUCONP_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ISUCONP_DB_PORT")
	if port == "" {
		port = "3306"
	}
	_, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Failed to read DB port number from an environment variable ISUCONP_DB_PORT.\nError: %s", err.Error())
	}
	user := os.Getenv("ISUCONP_DB_USER")
	if user == "" {
		user = "root"
	}
	password := os.Getenv("ISUCONP_DB_PASSWORD")
	dbname := os.Getenv("ISUCONP_DB_NAME")
	if dbname == "" {
		dbname = "isuconp"
	}

	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%s", host, port)
	cfg.DBName = dbname
	cfg.Params = map[string]string{
		"charset": "utf8mb4",
	}
	cfg.ParseTime = true
	cfg.Loc = time.Local
	dsn := cfg.FormatDSN()

	db, err = sqlx.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s.", err.Error())
	}
	defer db.Close()

	loadTemplates()

	r := chi.NewRouter()

	r.Get("/initialize", getInitialize)
	r.Get("/_internal/export-images", getExportImages)
	r.Get("/login", getLogin)
	r.Post("/login", postLogin)
	r.Get("/register", getRegister)
	r.Post("/register", postRegister)
	r.Get("/logout", getLogout)
	r.Get("/", getIndex)
	r.Get("/posts", getPosts)
	r.Get("/posts/{id}", getPostsID)
	r.Post("/", postIndex)
	r.Get("/image/{id}.{ext}", getImage)
	r.Post("/comment", postComment)
	r.Get("/admin/banned", getAdminBanned)
	r.Post("/admin/banned", postAdminBanned)
	r.Get(`/@{accountName:[0-9a-zA-Z_]+}`, getAccountName)
	r.Handle("/debug/pprof/*", http.DefaultServeMux)
	r.Mount("/", http.FileServer(http.Dir("../public")))

	log.Fatal(http.ListenAndServe(":8080", r))
}
