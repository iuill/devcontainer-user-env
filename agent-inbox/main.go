package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	defaultListen        = "127.0.0.1:3939"
	defaultContainerPath = "/inbox"
	defaultMaxBytes      = int64(20 << 20)
	multipartMemory      = int64(1 << 20)
	maxDimension         = 20000
	maxPixels            = 100_000_000
	mutationHeader       = "X-Agent-Inbox"
	shutdownTimeout      = 30 * time.Second
)

//go:embed web/*
var webFiles embed.FS

type app struct {
	dir           string
	containerPath string
	maxBytes      int64
	allowedUser   string
	now           func() time.Time
	random        io.Reader
}

type itemInfo struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Path    string    `json:"path"`
	URL     string    `json:"url"`
	Size    int64     `json:"size"`
	Time    string    `json:"time"`
	Width   int       `json:"width,omitempty"`
	Height  int       `json:"height,omitempty"`
	modTime time.Time `json:"-"`
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("determine home directory: %v", err)
	}

	listen := flag.String("listen", defaultListen, "address to listen on")
	defaultDir := os.Getenv("AGENT_INBOX_DIR")
	if defaultDir == "" {
		defaultDir = filepath.Join(home, "agent-inbox")
	}
	dir := flag.String("dir", defaultDir, "directory used to store shared files")
	containerPath := flag.String("container-path", defaultContainerPath, "path shown for Dev Container access")
	maxBytes := flag.Int64("max-bytes", defaultMaxBytes, "maximum image or text size in bytes")
	allowedUser := flag.String("allowed-user", os.Getenv("AGENT_INBOX_ALLOWED_USER"), "required Tailscale user login; empty disables the check")
	flag.Parse()

	if *maxBytes <= 0 {
		log.Fatal("-max-bytes must be greater than zero")
	}
	absoluteDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("resolve shared directory: %v", err)
	}
	if err := os.MkdirAll(absoluteDir, 0o700); err != nil {
		log.Fatalf("create shared directory: %v", err)
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           newApp(absoluteDir, *containerPath, *maxBytes, *allowedUser).routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()

	log.Printf("Agent Inbox listening on http://%s", *listen)
	log.Printf("Saving shared files in %s", absoluteDir)
	if *allowedUser != "" {
		log.Printf("Requiring Tailscale user %s", *allowedUser)
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newApp(dir, containerPath string, maxBytes int64, allowedUser string) *app {
	return &app{
		dir:           dir,
		containerPath: strings.TrimRight(containerPath, "/"),
		maxBytes:      maxBytes,
		allowedUser:   allowedUser,
		now:           time.Now,
		random:        rand.Reader,
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/items", a.listItems)
	mux.HandleFunc("POST /api/images", a.uploadImage)
	mux.HandleFunc("POST /api/texts", a.uploadText)
	mux.HandleFunc("DELETE /api/items/{name}", a.deleteItem)
	mux.HandleFunc("GET /files/{name}", a.serveItem)

	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(staticFiles)))
	return securityHeaders(a.authorize(mux))
}

func (a *app) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.allowedUser != "" && r.Header.Get("Tailscale-User-Login") != a.allowedUser {
			writeError(w, http.StatusForbidden, "このTailscaleユーザーにはアクセスが許可されていません")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) serveItem(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validStoredName(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.dir, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (a *app) listItems(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "共有ファイル一覧を読み込めませんでした")
		return
	}

	items := make([]itemInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validStoredName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		item := a.info(entry.Name(), info)
		if item.Kind == "image" {
			item.Width, item.Height = imageDimensions(filepath.Join(a.dir, entry.Name()))
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.After(items[j].modTime)
	})
	writeJSON(w, http.StatusOK, items)
}

func (a *app) uploadImage(w http.ResponseWriter, r *http.Request) {
	if !validMutation(r) {
		writeError(w, http.StatusForbidden, "不正なアップロード要求です")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.maxBytes+(1<<20))
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "画像がサイズ上限を超えています")
			return
		}
		writeError(w, http.StatusBadRequest, "アップロード形式が不正です")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像が指定されていません")
		return
	}
	defer file.Close()

	data, err := readLimited(file, a.maxBytes)
	if errors.Is(err, errTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "画像がサイズ上限を超えています")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像を読み込めませんでした")
		return
	}

	extension, width, height, err := validateImage(data)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}

	item, err := a.save(extension, data)
	if err != nil {
		log.Printf("save image: %v", err)
		writeError(w, http.StatusInternalServerError, "画像を保存できませんでした")
		return
	}
	item.Width = width
	item.Height = height
	writeJSON(w, http.StatusCreated, item)
}

func (a *app) uploadText(w http.ResponseWriter, r *http.Request) {
	if !validMutation(r) {
		writeError(w, http.StatusForbidden, "不正なアップロード要求です")
		return
	}
	if mediaType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]); mediaType != "text/plain" {
		writeError(w, http.StatusUnsupportedMediaType, "UTF-8のプレーンテキストだけを保存できます")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.maxBytes)
	data, err := readLimited(r.Body, a.maxBytes)
	if errors.Is(err, errTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "テキストがサイズ上限を超えています")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "テキストを読み込めませんでした")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "テキストが空です")
		return
	}
	if !utf8.Valid(data) {
		writeError(w, http.StatusUnsupportedMediaType, "UTF-8のプレーンテキストだけを保存できます")
		return
	}

	item, err := a.save(".txt", data)
	if err != nil {
		log.Printf("save text: %v", err)
		writeError(w, http.StatusInternalServerError, "テキストを保存できませんでした")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *app) save(extension string, data []byte) (itemInfo, error) {
	name, path, err := a.createFile(extension, data)
	if err != nil {
		return itemInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return itemInfo{}, err
	}
	return a.info(name, info), nil
}

func (a *app) deleteItem(w http.ResponseWriter, r *http.Request) {
	if !validMutation(r) {
		writeError(w, http.StatusForbidden, "不正な削除要求です")
		return
	}
	name := r.PathValue("name")
	if !validStoredName(name) {
		writeError(w, http.StatusBadRequest, "ファイル名が不正です")
		return
	}

	path := filepath.Join(a.dir, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "共有ファイルが見つかりません")
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "削除対象が不正です")
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, "共有ファイルを削除できませんでした")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validMutation(r *http.Request) bool {
	if r.Header.Get(mutationHeader) != "1" {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

var errTooLarge = errors.New("content exceeds size limit")

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, errTooLarge
		}
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errTooLarge
	}
	return data, nil
}

func validateImage(data []byte) (string, int, int, error) {
	contentType := http.DetectContentType(data)
	extensions := map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
	}
	extension, ok := extensions[contentType]
	if !ok {
		return "", 0, 0, errors.New("PNG、JPEG、GIF画像だけを保存できます")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != contentType {
		return "", 0, 0, errors.New("画像データが壊れています")
	}
	// DecodeConfig reads only metadata. The limits mainly protect clients that render saved files.
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxDimension || config.Height > maxDimension ||
		int64(config.Width)*int64(config.Height) > maxPixels {
		return "", 0, 0, errors.New("画像の寸法が上限を超えています")
	}
	return extension, config.Width, config.Height, nil
}

func imageDimensions(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func (a *app) createFile(extension string, data []byte) (string, string, error) {
	for range 10 {
		randomBytes := make([]byte, 4)
		if _, err := io.ReadFull(a.random, randomBytes); err != nil {
			return "", "", err
		}
		name := fmt.Sprintf(
			"%s-%s%s",
			a.now().Format("20060102-150405.000"),
			hex.EncodeToString(randomBytes),
			extension,
		)
		path := filepath.Join(a.dir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			os.Remove(path)
			return "", "", err
		}
		if err := file.Close(); err != nil {
			os.Remove(path)
			return "", "", err
		}
		return name, path, nil
	}
	return "", "", errors.New("could not create a unique filename")
}

func validStoredName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".gif", ".txt":
		return true
	default:
		return false
	}
}

func (a *app) info(name string, info os.FileInfo) itemInfo {
	kind := "image"
	if strings.EqualFold(filepath.Ext(name), ".txt") {
		kind = "text"
	}
	return itemInfo{
		Name:    name,
		Kind:    kind,
		Path:    a.containerPath + "/" + name,
		URL:     "/files/" + name,
		Size:    info.Size(),
		Time:    info.ModTime().UTC().Format(time.RFC3339),
		modTime: info.ModTime(),
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
