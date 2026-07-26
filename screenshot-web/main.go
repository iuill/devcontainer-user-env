package main

import (
	"bytes"
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultListen        = "127.0.0.1:3939"
	defaultContainerPath = "/screenshots"
	defaultMaxBytes      = int64(20 << 20)
	maxDimension         = 20000
	maxPixels            = 100_000_000
	mutationHeader       = "X-Screenshot-Web"
)

//go:embed web/*
var webFiles embed.FS

type app struct {
	dir           string
	containerPath string
	maxBytes      int64
	now           func() time.Time
	random        io.Reader
}

type imageInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	Time string `json:"time"`
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("determine home directory: %v", err)
	}

	listen := flag.String("listen", defaultListen, "address to listen on")
	defaultDir := os.Getenv("SCREENSHOT_WEB_DIR")
	if defaultDir == "" {
		defaultDir = filepath.Join(home, "screenshots")
	}
	dir := flag.String("dir", defaultDir, "directory used to store screenshots")
	containerPath := flag.String("container-path", defaultContainerPath, "path shown for Dev Container access")
	maxBytes := flag.Int64("max-bytes", defaultMaxBytes, "maximum image size in bytes")
	flag.Parse()

	if *maxBytes <= 0 {
		log.Fatal("-max-bytes must be greater than zero")
	}
	absoluteDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("resolve screenshot directory: %v", err)
	}
	if err := os.MkdirAll(absoluteDir, 0o700); err != nil {
		log.Fatalf("create screenshot directory: %v", err)
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           newApp(absoluteDir, *containerPath, *maxBytes).routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	log.Printf("Screenshot Web listening on http://%s", *listen)
	log.Printf("Saving images in %s", absoluteDir)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newApp(dir, containerPath string, maxBytes int64) *app {
	return &app{
		dir:           dir,
		containerPath: strings.TrimRight(containerPath, "/"),
		maxBytes:      maxBytes,
		now:           time.Now,
		random:        rand.Reader,
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/images", a.listImages)
	mux.HandleFunc("POST /api/images", a.uploadImage)
	mux.HandleFunc("DELETE /api/images/{name}", a.deleteImage)
	mux.HandleFunc("GET /images/{name}", a.serveImage)

	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(staticFiles)))
	return securityHeaders(mux)
}

func (a *app) serveImage(w http.ResponseWriter, r *http.Request) {
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

func (a *app) listImages(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "画像一覧を読み込めませんでした")
		return
	}

	images := make([]imageInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validStoredName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		images = append(images, a.info(entry.Name(), info))
	}
	sort.Slice(images, func(i, j int) bool {
		return images[i].Time > images[j].Time
	})
	writeJSON(w, http.StatusOK, images)
}

func (a *app) uploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(mutationHeader) != "1" {
		writeError(w, http.StatusForbidden, "不正なアップロード要求です")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.maxBytes+(1<<20))
	if err := r.ParseMultipartForm(a.maxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "画像が大きすぎるか、アップロード形式が不正です")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像が指定されていません")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, a.maxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像を読み込めませんでした")
		return
	}
	if int64(len(data)) > a.maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "画像がサイズ上限を超えています")
		return
	}

	extension, err := validateImage(data)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}

	name, path, err := a.createFile(extension, data)
	if err != nil {
		log.Printf("save image: %v", err)
		writeError(w, http.StatusInternalServerError, "画像を保存できませんでした")
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存した画像を確認できませんでした")
		return
	}
	writeJSON(w, http.StatusCreated, a.info(name, info))
}

func (a *app) deleteImage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(mutationHeader) != "1" {
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
		writeError(w, http.StatusNotFound, "画像が見つかりません")
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "削除対象が不正です")
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, "画像を削除できませんでした")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateImage(data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	extensions := map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
	}
	extension, ok := extensions[contentType]
	if !ok {
		return "", errors.New("PNG、JPEG、GIF画像だけを保存できます")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != contentType {
		return "", errors.New("画像データが壊れています")
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxDimension || config.Height > maxDimension ||
		int64(config.Width)*int64(config.Height) > maxPixels {
		return "", errors.New("画像の寸法が上限を超えています")
	}
	return extension, nil
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
	case ".png", ".jpg", ".gif":
		return true
	default:
		return false
	}
}

func (a *app) info(name string, info os.FileInfo) imageInfo {
	return imageInfo{
		Name: name,
		Path: a.containerPath + "/" + name,
		URL:  "/images/" + name,
		Size: info.Size(),
		Time: info.ModTime().Format(time.RFC3339),
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
