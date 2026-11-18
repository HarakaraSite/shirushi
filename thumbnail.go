package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	thumbnailCacheDir   = "thumbnail-cache"
	thumbnailVersion    = "1" // 出力サイズ・形式を変えたときに更新する。
	thumbnailMaxSide    = 640
	thumbnailMaxBytes   = 8 << 20
	thumbnailMaxPixels  = 8_000_000
	thumbnailCacheTTL   = 30 * 24 * time.Hour
	thumbnailRetryDelay = time.Hour
	thumbnailJobTimeout = 12 * time.Second
)

var bookmarkThumbnails *thumbnailCache

type thumbnailCache struct {
	dir    string
	client *http.Client
	slots  chan struct{}
	mu     sync.Mutex
	jobs   map[string]chan struct{}
}

func newThumbnailCache(dir string, client *http.Client) *thumbnailCache {
	return &thumbnailCache{
		dir: dir, client: client,
		slots: make(chan struct{}, 3), jobs: make(map[string]chan struct{}),
	}
}

func thumbnailCacheKey(source string) [sha256.Size]byte {
	return sha256.Sum256([]byte(thumbnailVersion + "\n" + source))
}

func thumbnailSource(rawURL string) string {
	source := strings.TrimSpace(rawURL)
	if _, err := validateHTTPURL(source); err != nil {
		return ""
	}
	return source
}

func bookmarkThumbnailURL(id int, rawURL string) string {
	source := thumbnailSource(rawURL)
	if source == "" {
		return ""
	}
	// 同じ画面内でも画像URLの変更後は、ブラウザが古い画像を再利用しない。
	key := thumbnailCacheKey(source)
	return fmt.Sprintf("/api/bookmarks/%d/thumbnail?v=%x", id, key[:16])
}

func (c *thumbnailCache) serve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}
	var source string
	err = db.QueryRowContext(r.Context(), `SELECT image_url FROM bookmarks WHERE id = ?`, id).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "画像URLの取得に失敗しました", http.StatusInternalServerError)
		return
	}
	source = thumbnailSource(source)
	if source == "" {
		http.NotFound(w, r)
		return
	}

	name, done := c.prepare(source)
	file, err := os.OpenInRoot(c.dir, name)
	// 期限切れの画像がある場合はそれをすぐに配信し、更新処理を待たない。
	if errors.Is(err, os.ErrNotExist) && done != nil {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			file, err = os.OpenInRoot(c.dir, name)
		}
	}
	if err != nil {
		// 未対応形式や元サイトがサーバーからの取得を拒む場合は従来の表示に戻す。
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, source, http.StatusFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "キャッシュ画像を読み込めません", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%x-%x"`, info.Name(), info.ModTime().UnixNano(), info.Size()))
	http.ServeContent(w, r, "thumbnail.jpg", time.Time{}, file)
}

func (c *thumbnailCache) warm(rawURL string) {
	if c == nil {
		return
	}
	if source := thumbnailSource(rawURL); source != "" {
		c.prepare(source)
	}
}

// prepare は同じ画像の生成をまとめ、ブラウザの再読み込みから独立して実行する。
func (c *thumbnailCache) prepare(source string) (string, <-chan struct{}) {
	name := fmt.Sprintf("%x.jpg", thumbnailCacheKey(source))
	path := filepath.Join(c.dir, name)
	if thumbnailFileFresh(path, thumbnailCacheTTL) || thumbnailFileFresh(path+".failed", thumbnailRetryDelay) {
		return name, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if done := c.jobs[path]; done != nil {
		return name, done
	}
	done := make(chan struct{})
	c.jobs[path] = done
	go c.generate(source, path, done)
	return name, done
}

func thumbnailFileFresh(path string, maxAge time.Duration) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < maxAge
}

func (c *thumbnailCache) generate(source, path string, done chan struct{}) {
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.jobs, path)
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailJobTimeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return
	}
	failedPath := path + ".failed"
	// スロット待ちや連続アクセスの間に他のジョブが済んでいれば取得しない。
	if ctx.Err() != nil || thumbnailFileFresh(path, thumbnailCacheTTL) || thumbnailFileFresh(failedPath, thumbnailRetryDelay) {
		return
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return
	}
	if err := c.download(ctx, source, path); err != nil {
		// 元画像が欠けているときも、一覧を開くたびに取り直すのを避ける。
		_ = os.WriteFile(failedPath, nil, 0o600)
		return
	}
	_ = os.Remove(failedPath)
}

func (c *thumbnailCache) download(ctx context.Context, source, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/*;q=0.8,*/*;q=0.5")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Shirushi/1.0)")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("画像のHTTPステータス: %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, thumbnailMaxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > thumbnailMaxBytes {
		return errors.New("画像のファイルサイズが上限を超えています")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > thumbnailMaxPixels/config.Height {
		return errors.New("画像のピクセル数が上限を超えています")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}

	width, height := config.Width, config.Height
	if longest := max(width, height); longest > thumbnailMaxSide {
		width = max(1, width*thumbnailMaxSide/longest)
		height = max(1, height*thumbnailMaxSide/longest)
	}
	thumb := image.NewRGBA(image.Rect(0, 0, width, height))
	background := image.NewUniform(color.RGBA{R: 30, G: 30, B: 30, A: 255})
	draw.Draw(thumb, thumb.Bounds(), background, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), src, src.Bounds(), draw.Over, nil)

	file, err := os.CreateTemp(c.dir, ".thumbnail-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encodeErr := jpeg.Encode(file, thumb, &jpeg.Options{Quality: 75})
	closeErr := file.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
