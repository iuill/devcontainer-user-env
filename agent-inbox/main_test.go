package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testImage(t *testing.T, format string) []byte {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var err error
	switch format {
	case "png":
		err = png.Encode(&data, img)
	case "jpeg":
		err = jpeg.Encode(&data, img, nil)
	case "gif":
		err = gif.Encode(&data, img, nil)
	default:
		t.Fatalf("unsupported test format: %s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func imageUploadRequest(t *testing.T, data []byte, withHeader bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "clipboard.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/images", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if withHeader {
		request.Header.Set(mutationHeader, "1")
	}
	return request
}

func textUploadRequest(text string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/texts", strings.NewReader(text))
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	request.Header.Set(mutationHeader, "1")
	return request
}

func testApp(dir string, maxBytes int64) *app {
	return newApp(dir, "/inbox", dir, maxBytes, "")
}

func TestUploadListAndDeleteImage(t *testing.T) {
	dir := t.TempDir()
	app := testApp(dir, defaultMaxBytes)
	app.now = func() time.Time {
		return time.Date(2026, 7, 26, 12, 34, 56, 789_000_000, time.UTC)
	}
	app.random = bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	handler := app.routes()

	upload := httptest.NewRecorder()
	handler.ServeHTTP(upload, imageUploadRequest(t, testImage(t, "png"), true))
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var created itemInfo
	if err := json.NewDecoder(upload.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "20260726-123456.789-01020304.png" {
		t.Fatalf("unexpected name: %q", created.Name)
	}
	if created.Kind != "image" {
		t.Fatalf("unexpected item: %#v", created)
	}
	if created.HostPath != filepath.Join(dir, created.Name) ||
		created.ContainerPath != "/inbox/"+created.Name {
		t.Fatalf("unexpected paths: %#v", created)
	}
	if created.Width != 2 || created.Height != 2 {
		t.Fatalf("unexpected dimensions: %dx%d", created.Width, created.Height)
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.Name) {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}

	remove := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/items/"+created.Name, nil)
	request.Header.Set(mutationHeader, "1")
	handler.ServeHTTP(remove, request)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", remove.Code, remove.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, created.Name)); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestUploadText(t *testing.T) {
	dir := t.TempDir()
	app := testApp(dir, defaultMaxBytes)
	app.now = func() time.Time { return time.Unix(0, 0) }
	app.random = bytes.NewReader([]byte{1, 2, 3, 4})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, textUploadRequest("長いテキスト\nsecond line"))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var created itemInfo
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Kind != "text" || filepath.Ext(created.Name) != ".txt" {
		t.Fatalf("unexpected item: %#v", created)
	}
	data, err := os.ReadFile(filepath.Join(dir, created.Name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "長いテキスト\nsecond line" {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestUploadTextPreservesAllowedExtension(t *testing.T) {
	for _, extension := range []string{".md", ".json", ".yaml", ".yml", ".csv", ".log", ".diff", ".patch"} {
		t.Run(extension, func(t *testing.T) {
			app := testApp(t.TempDir(), defaultMaxBytes)
			app.random = bytes.NewReader([]byte{1, 2, 3, 4})
			request := textUploadRequest("UTF-8 text")
			request.Header.Set(textExtensionHeader, extension)
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var created itemInfo
			if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(created.Name) != extension || created.Kind != "text" {
				t.Fatalf("unexpected item: %#v", created)
			}
		})
	}
}

func TestUploadTextValidation(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
		wantStatus  int
	}{
		{"mixed case media type", "Text/Plain; Charset=UTF-8", []byte("valid"), http.StatusCreated},
		{"empty", "text/plain", nil, http.StatusBadRequest},
		{"wrong media type", "application/json", []byte("{}"), http.StatusUnsupportedMediaType},
		{"invalid media type", "not a media type", []byte("text"), http.StatusUnsupportedMediaType},
		{"invalid UTF-8", "text/plain", []byte{0xff, 0xfe}, http.StatusUnsupportedMediaType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := testApp(t.TempDir(), defaultMaxBytes)
			app.random = bytes.NewReader([]byte{1, 2, 3, 4})
			request := httptest.NewRequest(http.MethodPost, "/api/texts", bytes.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set(mutationHeader, "1")
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}

	request := textUploadRequest("text")
	request.Header.Set(textExtensionHeader, ".html")
	response := httptest.NewRecorder()
	testApp(t.TempDir(), defaultMaxBytes).routes().ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported extension status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMutationProtection(t *testing.T) {
	app := testApp(t.TempDir(), defaultMaxBytes)
	tests := []struct {
		name    string
		request *http.Request
	}{
		{"missing header", imageUploadRequest(t, testImage(t, "png"), false)},
		{"cross site", imageUploadRequest(t, testImage(t, "png"), true)},
		{"foreign origin", imageUploadRequest(t, testImage(t, "png"), true)},
	}
	tests[1].request.Header.Set("Sec-Fetch-Site", "cross-site")
	tests[2].request.Header.Set("Origin", "https://evil.example")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, test.request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestAllowedTailscaleUser(t *testing.T) {
	dir := t.TempDir()
	app := newApp(dir, "/inbox", dir, defaultMaxBytes, "owner@example.com")
	handler := app.routes()
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d", denied.Code)
	}
	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set("Tailscale-User-Login", "owner@example.com")
	handler.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, body = %s", allowed.Code, allowed.Body.String())
	}
}

func TestUploadSizeLimitReturns413(t *testing.T) {
	app := testApp(t.TempDir(), 16)
	imageResponse := httptest.NewRecorder()
	app.routes().ServeHTTP(imageResponse, imageUploadRequest(t, testImage(t, "png"), true))
	if imageResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("image status = %d, body = %s", imageResponse.Code, imageResponse.Body.String())
	}

	textResponse := httptest.NewRecorder()
	app.routes().ServeHTTP(textResponse, textUploadRequest(strings.Repeat("x", 17)))
	if textResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("text status = %d, body = %s", textResponse.Code, textResponse.Body.String())
	}
}

func TestMultipartBodyLimitReturns413(t *testing.T) {
	app := testApp(t.TempDir(), 16)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormField("padding")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, strings.Repeat("x", int(16+(1<<20)+1))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/images", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set(mutationHeader, "1")
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}

func TestImageFormats(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			extension, width, height, err := validateImage(testImage(t, format))
			if err != nil {
				t.Fatal(err)
			}
			if extension == "" || width != 2 || height != 2 {
				t.Fatalf("unexpected result: %q %dx%d", extension, width, height)
			}
		})
	}
	if _, _, _, err := validateImage([]byte("not an image")); err == nil {
		t.Fatal("non-image unexpectedly accepted")
	}
}

func TestRejectsOversizedDimensions(t *testing.T) {
	data := minimalPNG(maxDimension+1, 1)
	if _, _, _, err := validateImage(data); err == nil {
		t.Fatal("oversized image unexpectedly accepted")
	}
	data = minimalPNG(10001, 10000)
	if _, _, _, err := validateImage(data); err == nil {
		t.Fatal("image over pixel limit unexpectedly accepted")
	}
}

func minimalPNG(width, height int) []byte {
	var result bytes.Buffer
	result.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	payload := make([]byte, 13)
	binary.BigEndian.PutUint32(payload[0:4], uint32(width))
	binary.BigEndian.PutUint32(payload[4:8], uint32(height))
	payload[8] = 8
	payload[9] = 6
	writePNGChunk(&result, "IHDR", payload)
	writePNGChunk(&result, "IEND", nil)
	return result.Bytes()
}

func writePNGChunk(writer io.Writer, kind string, payload []byte) {
	binary.Write(writer, binary.BigEndian, uint32(len(payload)))
	writer.Write([]byte(kind))
	writer.Write(payload)
	checksum := crc32.NewIEEE()
	checksum.Write([]byte(kind))
	checksum.Write(payload)
	binary.Write(writer, binary.BigEndian, checksum.Sum32())
}

func TestValidStoredName(t *testing.T) {
	tests := map[string]bool{
		"image.png":   true,
		"image.PNG":   true,
		"note.txt":    true,
		"note.md":     true,
		"data.json":   true,
		"config.yaml": true,
		"config.yml":  true,
		"table.csv":   true,
		"output.log":  true,
		"change.diff": true,
		"fix.patch":   true,
		".png":        true,
		"..":          false,
		"a/b.png":     false,
		`a\b.png`:     false,
		"image.svg":   false,
		"page.html":   false,
		"noext":       false,
	}
	for name, want := range tests {
		if got := validStoredName(name); got != want {
			t.Errorf("validStoredName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestListFiltersAndSortsItems(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, testImage(t, "png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldPath, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("old", -5*60*60))
	newTime := time.Date(2026, 1, 1, 2, 0, 0, 0, time.FixedZone("new", 9*60*60))
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	app := testApp(dir, defaultMaxBytes)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	var items []itemInfo
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "old.txt" || items[1].Name != "new.png" {
		t.Fatalf("unexpected sorted items: %#v", items)
	}
	if items[0].Snippet != "old" {
		t.Fatalf("unexpected text snippet: %q", items[0].Snippet)
	}
	if len(app.metadata) != 2 {
		t.Fatalf("metadata cache contains %d entries, want 2", len(app.metadata))
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if _, ok := app.metadata["old.txt"]; ok {
		t.Fatal("metadata for externally removed file was not pruned")
	}
}

func TestDeleteRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)
	decoy := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(decoy, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(decoy) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/items/%2e%2e%2fsecret.txt", nil)
	request.Header.Set(mutationHeader, "1")
	testApp(dir, defaultMaxBytes).routes().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("decoy was removed: %v", err)
	}
}

func TestDeleteRejectsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.txt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"folder.txt", "link.txt"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodDelete, "/api/items/"+name, nil)
			request.Header.Set(mutationHeader, "1")
			response := httptest.NewRecorder()
			testApp(dir, defaultMaxBytes).routes().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
		t.Fatalf("target changed: %q, %v", data, err)
	}
}

func TestFilesAreServedWithSecurityAndCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	data := testImage(t, "png")
	if err := os.WriteFile(filepath.Join(dir, "test.png"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	testApp(dir, defaultMaxBytes).routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/files/test.png", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff header is missing")
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("immutable cache header is missing")
	}
}

func TestTextFilesAreServedAsPlainText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte(`{"safe":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	testApp(dir, defaultMaxBytes).routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/files/test.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}

func TestCreateFileRetriesCollision(t *testing.T) {
	dir := t.TempDir()
	app := testApp(dir, defaultMaxBytes)
	app.now = func() time.Time { return time.Unix(0, 0) }
	app.random = bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	first := "19700101-000000.000-01020304.txt"
	if err := os.WriteFile(filepath.Join(dir, first), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, _, err := app.createFile(".txt", []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "19700101-000000.000-05060708.txt" {
		t.Fatalf("unexpected retried name: %s", name)
	}
}

func TestConfigAndHTMLAuthorizationError(t *testing.T) {
	dir := t.TempDir()
	app := newApp(dir, "/inbox", dir, 1234, "owner@example.com")
	denied := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	app.routes().ServeHTTP(denied, request)
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("unexpected HTML denial: status=%d content-type=%q", denied.Code, denied.Header().Get("Content-Type"))
	}

	config := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	request.Header.Set("Tailscale-User-Login", "owner@example.com")
	app.routes().ServeHTTP(config, request)
	if config.Code != http.StatusOK || !strings.Contains(config.Body.String(), "1234") {
		t.Fatalf("unexpected config response: status=%d body=%s", config.Code, config.Body.String())
	}
}

func TestTextSnippetDropsIncompleteTrailingRune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boundary.txt")
	content := strings.Repeat(" ", 4095) + "あ"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snippet := textSnippet(path)
	if strings.ContainsRune(snippet, '�') {
		t.Fatalf("snippet contains a replacement rune: %q", snippet)
	}
}

func TestListSourceDirectory(t *testing.T) {
	inbox := t.TempDir()
	source := t.TempDir()
	project := filepath.Join(source, "project")
	if err := os.MkdirAll(filepath.Join(project, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"archive.bin":    []byte{0x00, 0x01},
		"diagram.svg":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"main.go":        []byte("package main\n"),
		"notes.markdown": []byte("# Notes\n"),
		".env":           []byte("SECRET=hidden\n"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(project, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(project, "main.go"), filepath.Join(project, "linked.go")); err != nil {
		t.Fatal(err)
	}

	app := newApp(inbox, "/inbox", source, defaultMaxBytes, "")
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/source?path=project", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var listing sourceListing
	if err := json.NewDecoder(response.Body).Decode(&listing); err != nil {
		t.Fatal(err)
	}
	if listing.Path != "project" || listing.Parent == nil || *listing.Parent != "" {
		t.Fatalf("unexpected location: %#v", listing)
	}
	if len(listing.Entries) != 5 {
		t.Fatalf("entries = %#v", listing.Entries)
	}
	wants := []struct {
		name string
		kind string
	}{
		{"assets", "directory"},
		{"archive.bin", "file"},
		{"diagram.svg", "image"},
		{"main.go", "text"},
		{"notes.markdown", "text"},
	}
	for index, want := range wants {
		got := listing.Entries[index]
		if got.Name != want.name || got.Kind != want.kind {
			t.Fatalf("entry %d = %#v, want %s/%s", index, got, want.name, want.kind)
		}
		if got.HostPath != filepath.Join(project, want.name) {
			t.Fatalf("host path = %q", got.HostPath)
		}
	}
	if listing.Entries[2].URL != "/source/project/diagram.svg" {
		t.Fatalf("svg URL = %q", listing.Entries[2].URL)
	}
}

func TestSourceFilesAreServedSafely(t *testing.T) {
	inbox := t.TempDir()
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "project", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	maliciousSVG := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	if err := os.WriteFile(filepath.Join(source, "project", "preview.svg"), []byte(maliciousSVG), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newApp(inbox, "/inbox", source, defaultMaxBytes, "").routes()

	textResponse := httptest.NewRecorder()
	handler.ServeHTTP(textResponse, httptest.NewRequest(http.MethodGet, "/source/project/main.go", nil))
	if textResponse.Code != http.StatusOK || textResponse.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("text response: status=%d content-type=%q", textResponse.Code, textResponse.Header().Get("Content-Type"))
	}
	if !strings.Contains(textResponse.Header().Get("Cache-Control"), "no-cache") {
		t.Fatal("source response must not use immutable caching")
	}

	svgResponse := httptest.NewRecorder()
	handler.ServeHTTP(svgResponse, httptest.NewRequest(http.MethodGet, "/source/project/preview.svg", nil))
	if svgResponse.Code != http.StatusOK || svgResponse.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("svg response: status=%d content-type=%q", svgResponse.Code, svgResponse.Header().Get("Content-Type"))
	}
	if !strings.Contains(svgResponse.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("SVG CSP = %q", svgResponse.Header().Get("Content-Security-Policy"))
	}

	limitedHandler := newApp(inbox, "/inbox", source, 4, "").routes()
	largeTextResponse := httptest.NewRecorder()
	limitedHandler.ServeHTTP(largeTextResponse, httptest.NewRequest(http.MethodGet, "/source/project/main.go", nil))
	if largeTextResponse.Code != http.StatusOK || largeTextResponse.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("large text response: status=%d content-type=%q", largeTextResponse.Code, largeTextResponse.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(largeTextResponse.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("large text disposition = %q", largeTextResponse.Header().Get("Content-Disposition"))
	}
}

func TestSourceBrowserRejectsTraversalHiddenFilesAndSymlinks(t *testing.T) {
	inbox := t.TempDir()
	source := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}
	handler := newApp(inbox, "/inbox", source, defaultMaxBytes, "").routes()

	for _, target := range []string{
		"/api/source?path=../secret",
		"/api/source?path=.git",
		"/api/source?path=linked",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", target, response.Code)
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/source/linked/secret.txt", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("symlink file status = %d, want 404", response.Code)
	}
}

func TestSourceFileKind(t *testing.T) {
	tests := map[string]string{
		"preview.svg": "image",
		"photo.webp":  "image",
		"main.go":     "text",
		"go.mod":      "text",
		"Dockerfile":  "text",
		"pnpm-lock":   "file",
		"archive.zip": "file",
	}
	for name, want := range tests {
		if got := sourceFileKind(name); got != want {
			t.Errorf("sourceFileKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSourceEntryIncludesZeroSize(t *testing.T) {
	data, err := json.Marshal(sourceEntry{Name: "empty.txt", Size: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"size":0`) {
		t.Fatalf("zero size is missing: %s", data)
	}
}
