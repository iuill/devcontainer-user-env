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
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const (
	defaultListen        = "127.0.0.1:3939"
	defaultContainerPath = "/inbox"
	defaultMaxBytes      = int64(20 << 20)
	defaultMaxFileBytes  = int64(500 << 20)
	multipartMemory      = int64(1 << 20)
	maxDimension         = 20000
	maxPixels            = 100_000_000
	mutationHeader       = "X-Agent-Inbox"
	textExtensionHeader  = "X-Agent-Inbox-Extension"
	shutdownTimeout      = 30 * time.Second
	uploadTimeout        = 10 * time.Minute
	textSnippetRunes     = 180
	markdownPreviewBytes = int64(500 * 1024)
)

//go:embed web/*
var webFiles embed.FS

type app struct {
	dir           string
	containerPath string
	sourceDir     string
	maxBytes      int64
	maxFileBytes  int64
	allowedUser   string
	now           func() time.Time
	random        io.Reader
	metadataMu    sync.Mutex
	metadata      map[string]metadataCache
}

type sourceEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	HostPath string `json:"hostPath"`
	URL      string `json:"url,omitempty"`
	Size     int64  `json:"size"`
	Time     string `json:"time"`
}

type sourceListing struct {
	Path    string        `json:"path"`
	Parent  *string       `json:"parent"`
	Entries []sourceEntry `json:"entries"`
}

type itemInfo struct {
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	HostPath      string    `json:"hostPath"`
	ContainerPath string    `json:"containerPath"`
	URL           string    `json:"url"`
	Size          int64     `json:"size"`
	Time          string    `json:"time"`
	Width         int       `json:"width,omitempty"`
	Height        int       `json:"height,omitempty"`
	Snippet       string    `json:"snippet,omitempty"`
	modTime       time.Time `json:"-"`
}

type metadataCache struct {
	size    int64
	modTime int64
	width   int
	height  int
	snippet string
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
	defaultSourceDir := os.Getenv("AGENT_INBOX_SOURCE_DIR")
	if defaultSourceDir == "" {
		defaultSourceDir = filepath.Join(home, "src")
	}
	sourceDir := flag.String("source-dir", defaultSourceDir, "read-only directory exposed by the source browser")
	maxBytes := flag.Int64("max-bytes", defaultMaxBytes, "maximum image or text size in bytes")
	maxFileBytes := flag.Int64("max-file-bytes", defaultMaxFileBytes, "maximum generic file size in bytes")
	allowedUser := flag.String("allowed-user", os.Getenv("AGENT_INBOX_ALLOWED_USER"), "required Tailscale user login; empty disables the check")
	flag.Parse()

	if *maxBytes <= 0 {
		log.Fatal("-max-bytes must be greater than zero")
	}
	if *maxFileBytes <= 0 {
		log.Fatal("-max-file-bytes must be greater than zero")
	}
	absoluteDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("resolve shared directory: %v", err)
	}
	if err := os.MkdirAll(absoluteDir, 0o700); err != nil {
		log.Fatalf("create shared directory: %v", err)
	}
	absoluteSourceDir, err := filepath.Abs(*sourceDir)
	if err != nil {
		log.Fatalf("resolve source directory: %v", err)
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(absoluteSourceDir); evaluateErr == nil {
		absoluteSourceDir = evaluated
	} else if !errors.Is(evaluateErr, os.ErrNotExist) {
		log.Fatalf("resolve source directory symlinks: %v", evaluateErr)
	}

	application := newApp(absoluteDir, *containerPath, absoluteSourceDir, *maxBytes, *allowedUser)
	application.maxFileBytes = *maxFileBytes
	server := &http.Server{
		Addr:              *listen,
		Handler:           application.routes(),
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
	log.Printf("Browsing source files in %s", absoluteSourceDir)
	if *allowedUser != "" {
		log.Printf("Requiring Tailscale user %s", *allowedUser)
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newApp(dir, containerPath, sourceDir string, maxBytes int64, allowedUser string) *app {
	return &app{
		dir:           dir,
		containerPath: strings.TrimRight(containerPath, "/"),
		sourceDir:     sourceDir,
		maxBytes:      maxBytes,
		maxFileBytes:  defaultMaxFileBytes,
		allowedUser:   allowedUser,
		now:           time.Now,
		random:        rand.Reader,
		metadata:      make(map[string]metadataCache),
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", a.getConfig)
	mux.HandleFunc("GET /api/items", a.listItems)
	mux.HandleFunc("GET /api/source", a.listSource)
	mux.HandleFunc("GET /api/source/markdown", a.renderSourceMarkdown)
	mux.HandleFunc("POST /api/images", a.uploadImage)
	mux.HandleFunc("POST /api/texts", a.uploadText)
	mux.HandleFunc("POST /api/files", a.uploadFile)
	mux.HandleFunc("DELETE /api/items/{name}", a.deleteItem)
	mux.HandleFunc("GET /files/{name}", a.serveItem)
	mux.HandleFunc("GET /source/{path...}", a.serveSource)

	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(staticFiles)))
	return securityHeaders(a.authorize(mux))
}

func (a *app) renderSourceMarkdown(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(a.sourceDir)
	if err != nil {
		writeError(w, http.StatusNotFound, "Markdownファイルが見つかりません")
		return
	}
	defer root.Close()

	relative, info, err := a.resolveSourcePath(root, r.URL.Query().Get("path"))
	if err != nil || !info.Mode().IsRegular() || !isSourceMarkdown(relative) {
		writeError(w, http.StatusNotFound, "Markdownファイルが見つかりません")
		return
	}
	if info.Size() > markdownPreviewBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "500 KiBを超えるMarkdownはプレビューできません")
		return
	}

	file, err := root.Open(relative)
	if err != nil {
		writeError(w, http.StatusNotFound, "Markdownファイルが見つかりません")
		return
	}
	defer file.Close()
	markdown, err := readLimited(file, markdownPreviewBytes)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "500 KiBを超えるMarkdownはプレビューできません")
			return
		}
		writeError(w, http.StatusInternalServerError, "Markdownを読み込めませんでした")
		return
	}

	var rendered bytes.Buffer
	converter := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := converter.Convert(markdown, &rendered); err != nil {
		writeError(w, http.StatusInternalServerError, "Markdownを表示できませんでした")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := rendered.WriteTo(w); err != nil {
		log.Printf("write Markdown preview: %v", err)
	}
}

func (a *app) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.allowedUser != "" && r.Header.Get("Tailscale-User-Login") != a.allowedUser {
			if r.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(r.Header.Get("Accept"), "text/html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "<!doctype html><html lang=\"ja\"><meta charset=\"utf-8\"><title>アクセス拒否</title><body><h1>アクセスできません</h1><p>このTailscaleユーザーにはアクセスが許可されていません。</p></body></html>")
				return
			}
			writeError(w, http.StatusForbidden, "このTailscaleユーザーにはアクセスが許可されていません")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"maxBytes":     a.maxBytes,
		"maxFileBytes": a.maxFileBytes,
		"sourceRoot":   a.sourceDir,
	})
}

func (a *app) listSource(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(a.sourceDir)
	if err != nil {
		writeError(w, http.StatusNotFound, "src内のディレクトリが見つかりません")
		return
	}
	defer root.Close()

	relative, info, err := a.resolveSourcePath(root, r.URL.Query().Get("path"))
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusNotFound, "src内のディレクトリが見つかりません")
		return
	}
	directory, err := root.Open(sourceRootName(relative))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "src内のファイル一覧を読み込めませんでした")
		return
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "src内のファイル一覧を読み込めませんでした")
		return
	}

	result := sourceListing{
		Path:    filepath.ToSlash(relative),
		Parent:  sourceParent(relative),
		Entries: make([]sourceEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || strings.Contains(entry.Name(), `\`) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil || (!entryInfo.IsDir() && !entryInfo.Mode().IsRegular()) {
			continue
		}
		entryPath := filepath.Join(relative, entry.Name())
		kind := sourceFileKind(entry.Name())
		if entryInfo.IsDir() {
			kind = "directory"
		} else if kind == "text" && entryInfo.Size() > a.maxBytes {
			kind = "file"
		}
		item := sourceEntry{
			Name:     entry.Name(),
			Path:     filepath.ToSlash(entryPath),
			Kind:     kind,
			HostPath: filepath.Join(a.sourceDir, entryPath),
			Time:     entryInfo.ModTime().UTC().Format(time.RFC3339),
		}
		if !entryInfo.IsDir() {
			item.Size = entryInfo.Size()
			item.URL = sourceURL(entryPath)
		}
		result.Entries = append(result.Entries, item)
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		if result.Entries[i].Kind == "directory" && result.Entries[j].Kind != "directory" {
			return true
		}
		if result.Entries[i].Kind != "directory" && result.Entries[j].Kind == "directory" {
			return false
		}
		return strings.ToLower(result.Entries[i].Name) < strings.ToLower(result.Entries[j].Name)
	})
	writeJSON(w, http.StatusOK, result)
}

func (a *app) serveSource(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(a.sourceDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()

	relative, info, err := a.resolveSourcePath(root, r.PathValue("path"))
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	file, err := root.Open(relative)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	extension := strings.ToLower(filepath.Ext(relative))
	kind := sourceFileKind(relative)
	if kind == "text" && info.Size() > a.maxBytes {
		kind = "file"
	}
	switch kind {
	case "image":
		w.Header().Set("Content-Type", sourceImageContentType(extension))
		w.Header().Set("Content-Disposition", "inline")
		if extension == ".svg" {
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		}
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "inline")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(relative)}))
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, filepath.Base(relative), info.ModTime(), file)
}

func (a *app) resolveSourcePath(root *os.Root, raw string) (string, os.FileInfo, error) {
	relative, err := cleanSourcePath(raw)
	if err != nil {
		return "", nil, err
	}
	info, err := root.Lstat(".")
	if err != nil || !info.IsDir() {
		return "", nil, os.ErrNotExist
	}
	if relative == "" {
		return relative, info, nil
	}
	current := ""
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err = root.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, os.ErrNotExist
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", nil, os.ErrNotExist
		}
	}
	return relative, info, nil
}

func sourceRootName(relative string) string {
	if relative == "" {
		return "."
	}
	return relative
}

func cleanSourcePath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.Contains(raw, `\`) || strings.HasPrefix(raw, "/") {
		return "", errors.New("invalid source path")
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return "", errors.New("invalid source path")
		}
	}
	return filepath.Join(parts...), nil
}

func sourceParent(relative string) *string {
	if relative == "" {
		return nil
	}
	parent := filepath.Dir(relative)
	if parent == "." {
		parent = ""
	}
	parent = filepath.ToSlash(parent)
	return &parent
}

func sourceURL(relative string) string {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return "/source/" + strings.Join(parts, "/")
}

func isSourceMarkdown(name string) bool {
	baseName := strings.ToLower(filepath.Base(name))
	return baseName == "readme" || strings.HasSuffix(baseName, ".md") || strings.HasSuffix(baseName, ".markdown")
}

// sourceFileKind is intentionally broader than isTextExtension: src files are
// only viewed, while inbox text extensions define which uploaded files may be stored.
func sourceFileKind(name string) string {
	switch strings.ToLower(filepath.Base(name)) {
	case "dockerfile", "containerfile", "makefile", "license", "notice", "readme", "go.mod", "go.sum":
		return "text"
	}
	extension := strings.ToLower(filepath.Ext(name))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg":
		return "image"
	case ".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".csv", ".log", ".diff", ".patch",
		".go", ".js", ".jsx", ".ts", ".tsx", ".css", ".scss", ".html", ".htm", ".xml",
		".sh", ".bash", ".zsh", ".py", ".rb", ".rs", ".java", ".kt", ".swift", ".c", ".h",
		".cc", ".cpp", ".hpp", ".toml", ".ini", ".conf", ".sql", ".graphql", ".vue", ".svelte",
		".mod", ".sum", ".lock", ".proto", ".properties", ".gradle", ".cs", ".fs", ".fsx", ".ex",
		".exs", ".lua", ".php", ".pl", ".r", ".dart", ".tf", ".hcl":
		return "text"
	default:
		return "file"
	}
}

func sourceImageContentType(extension string) string {
	switch extension {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
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
	kind := storedFileKind(name)
	if kind == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else if kind == "file" {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
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
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validStoredName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[entry.Name()] = struct{}{}
		item := a.info(entry.Name(), info)
		if item.Kind == "image" {
			item.Width, item.Height = a.cachedImageDimensions(entry.Name(), info)
		} else if item.Kind == "text" {
			item.Snippet = a.cachedTextSnippet(entry.Name(), info)
		}
		items = append(items, item)
	}
	a.pruneMetadata(seen)
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
	setUploadReadDeadline(w)

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
	a.cacheDimensions(item.Name, item.Size, item.modTime, width, height)
	writeJSON(w, http.StatusCreated, item)
}

func (a *app) uploadText(w http.ResponseWriter, r *http.Request) {
	if !validMutation(r) {
		writeError(w, http.StatusForbidden, "不正なアップロード要求です")
		return
	}
	setUploadReadDeadline(w)
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "text/plain") {
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
	extension, ok := normalizeTextExtension(r.Header.Get(textExtensionHeader))
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "対応していないテキストファイル形式です")
		return
	}

	item, err := a.save(extension, data)
	if err != nil {
		log.Printf("save text: %v", err)
		writeError(w, http.StatusInternalServerError, "テキストを保存できませんでした")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *app) uploadFile(w http.ResponseWriter, r *http.Request) {
	if !validMutation(r) {
		writeError(w, http.StatusForbidden, "不正なアップロード要求です")
		return
	}
	setUploadReadDeadline(w)

	r.Body = http.MaxBytesReader(w, r.Body, a.maxFileBytes+(1<<20))
	multipartReader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "アップロード形式が不正です")
		return
	}
	part, err := multipartReader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		writeError(w, http.StatusBadRequest, "ファイルが指定されていません")
		return
	}
	defer part.Close()

	item, err := a.saveReader(uploadedFileExtension(part.FileName()), part, a.maxFileBytes)
	if errors.Is(err, errTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "ファイルがサイズ上限を超えています")
		return
	}
	if err != nil {
		log.Printf("save file: %v", err)
		writeError(w, http.StatusInternalServerError, "ファイルを保存できませんでした")
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

func (a *app) saveReader(extension string, reader io.Reader, maxBytes int64) (itemInfo, error) {
	name, path, err := a.createFileFromReader(extension, reader, maxBytes)
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
	a.metadataMu.Lock()
	delete(a.metadata, name)
	a.metadataMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func setUploadReadDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadTimeout))
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

func imageDimensions(path string) (int, int, bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

func (a *app) cachedImageDimensions(name string, info os.FileInfo) (int, int) {
	a.metadataMu.Lock()
	cached, ok := a.metadata[name]
	a.metadataMu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime == info.ModTime().UnixNano() {
		return cached.width, cached.height
	}
	width, height, ok := imageDimensions(filepath.Join(a.dir, name))
	if !ok {
		return 0, 0
	}
	a.cacheDimensions(name, info.Size(), info.ModTime(), width, height)
	return width, height
}

func (a *app) cacheDimensions(name string, size int64, modTime time.Time, width, height int) {
	a.metadataMu.Lock()
	a.metadata[name] = metadataCache{
		size:    size,
		modTime: modTime.UnixNano(),
		width:   width,
		height:  height,
	}
	a.metadataMu.Unlock()
}

func (a *app) cachedTextSnippet(name string, info os.FileInfo) string {
	a.metadataMu.Lock()
	cached, ok := a.metadata[name]
	a.metadataMu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime == info.ModTime().UnixNano() {
		return cached.snippet
	}
	snippet := textSnippet(filepath.Join(a.dir, name))
	a.metadataMu.Lock()
	a.metadata[name] = metadataCache{
		size:    info.Size(),
		modTime: info.ModTime().UnixNano(),
		snippet: snippet,
	}
	a.metadataMu.Unlock()
	return snippet
}

func (a *app) pruneMetadata(seen map[string]struct{}) {
	a.metadataMu.Lock()
	defer a.metadataMu.Unlock()
	for name := range a.metadata {
		if _, ok := seen[name]; !ok {
			delete(a.metadata, name)
		}
	}
}

func textSnippet(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4096+utf8.UTFMax))
	if err != nil {
		return ""
	}
	if len(data) > 4096 {
		data = data[:4096]
		for len(data) > 0 {
			r, size := utf8.DecodeLastRune(data)
			if r != utf8.RuneError || size > 1 {
				break
			}
			data = data[:len(data)-1]
		}
	}
	text := strings.Join(strings.Fields(string(bytes.ToValidUTF8(data, []byte("�")))), " ")
	runes := []rune(text)
	if len(runes) > textSnippetRunes {
		return string(runes[:textSnippetRunes]) + "…"
	}
	return text
}

func (a *app) createFile(extension string, data []byte) (string, string, error) {
	return a.createFileFromReader(extension, bytes.NewReader(data), int64(len(data)))
}

func (a *app) createFileFromReader(extension string, reader io.Reader, maxBytes int64) (string, string, error) {
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
		written, writeErr := io.Copy(file, io.LimitReader(reader, maxBytes+1))
		if writeErr == nil && written > maxBytes {
			writeErr = errTooLarge
		}
		closeErr := file.Close()
		if writeErr != nil {
			os.Remove(path)
			return "", "", writeErr
		}
		if closeErr != nil {
			os.Remove(path)
			return "", "", closeErr
		}
		return name, path, nil
	}
	return "", "", errors.New("could not create a unique filename")
}

func validStoredName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return false
	}
	return name != "." && name != ".."
}

func uploadedFileExtension(name string) string {
	extension := strings.ToLower(filepath.Ext(filepath.Base(strings.ReplaceAll(name, `\`, "/"))))
	if len(extension) < 2 || len(extension) > 32 {
		return ".bin"
	}
	for _, character := range extension[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return ".bin"
		}
	}
	return extension
}

func normalizeTextExtension(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return ".txt", true
	}
	extension := strings.ToLower(strings.TrimSpace(value))
	return extension, isTextExtension(extension)
}

func isTextExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".csv", ".log", ".diff", ".patch":
		return true
	default:
		return false
	}
}

func (a *app) info(name string, info os.FileInfo) itemInfo {
	return itemInfo{
		Name:          name,
		Kind:          storedFileKind(name),
		HostPath:      filepath.Join(a.dir, name),
		ContainerPath: a.containerPath + "/" + name,
		URL:           "/files/" + name,
		Size:          info.Size(),
		Time:          info.ModTime().UTC().Format(time.RFC3339),
		modTime:       info.ModTime(),
	}
}

func storedFileKind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return "image"
	default:
		if isTextExtension(filepath.Ext(name)) {
			return "text"
		}
		return "file"
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
