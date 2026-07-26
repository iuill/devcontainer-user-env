package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
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

func testPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func uploadRequest(t *testing.T, data []byte, withHeader bool) *http.Request {
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

func TestUploadListAndDelete(t *testing.T) {
	dir := t.TempDir()
	app := newApp(dir, "/screenshots", defaultMaxBytes)
	app.now = func() time.Time {
		return time.Date(2026, 7, 26, 12, 34, 56, 789_000_000, time.UTC)
	}
	app.random = bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	handler := app.routes()

	upload := httptest.NewRecorder()
	handler.ServeHTTP(upload, uploadRequest(t, testPNG(t), true))
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var created imageInfo
	if err := json.NewDecoder(upload.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "20260726-123456.789-01020304.png" {
		t.Fatalf("unexpected name: %q", created.Name)
	}
	if created.Path != "/screenshots/"+created.Name {
		t.Fatalf("unexpected container path: %q", created.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, created.Name)); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/images", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.Name) {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}

	remove := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/images/"+created.Name, nil)
	request.Header.Set(mutationHeader, "1")
	handler.ServeHTTP(remove, request)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", remove.Code, remove.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, created.Name)); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestMutationHeaderIsRequired(t *testing.T) {
	app := newApp(t.TempDir(), "/screenshots", defaultMaxBytes)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, uploadRequest(t, testPNG(t), false))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestRejectsNonImage(t *testing.T) {
	app := newApp(t.TempDir(), "/screenshots", defaultMaxBytes)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, uploadRequest(t, []byte("not an image"), true))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestDeleteRejectsTraversal(t *testing.T) {
	app := newApp(t.TempDir(), "/screenshots", defaultMaxBytes)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/images/%2e%2e%2fsecret.png", nil)
	request.Header.Set(mutationHeader, "1")
	app.routes().ServeHTTP(response, request)
	if response.Code == http.StatusNoContent {
		t.Fatal("traversal delete unexpectedly succeeded")
	}
}

func TestImagesAreServed(t *testing.T) {
	dir := t.TempDir()
	data := testPNG(t)
	if err := os.WriteFile(filepath.Join(dir, "test.png"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	app := newApp(dir, "/screenshots", defaultMaxBytes)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/images/test.png", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	received, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, data) {
		t.Fatal("served image differs from stored image")
	}
}
