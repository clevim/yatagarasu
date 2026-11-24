package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func makeTestMangaPagePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 600))
	// Gray manga panel background
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{180, 180, 180, 255}), image.Point{}, draw.Src)
	// White speech bubble in top-left
	draw.Draw(img, image.Rect(40, 40, 200, 180), image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, draw.Src)
	// Dark dramatic bubble in bottom-right
	draw.Draw(img, image.Rect(200, 360, 360, 500), image.NewUniform(color.RGBA{20, 20, 20, 255}), image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test manga page: %v", err)
	}
	return buf.Bytes()
}

func TestBuiltinVisionPageTranslatorGeminiAndCache(t *testing.T) {
	var calls atomic.Int32
	mockGemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "inline_data") {
			t.Errorf("expected gemini inline_data image payload, got: %s", string(body))
		}
		writeJSON(w, map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{
								"text": `{"bubbles":[` +
									`{"box":[66,100,300,500],"text":"Não pode ser! É incrível!"},` +
									`{"box":[600,500,833,900],"text":"Silêncio..."}` +
									`]}`,
							},
						},
					},
				},
			},
		})
	}))
	defer mockGemini.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.TranslateProvider = "gemini"
	st.TranslateEndpoint = mockGemini.URL
	st.TranslateAPIKey = "test-key"
	st.ImageTranslatorLang = "PTB"
	_ = cfg.save(st)

	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. /api/health reports built-in mode as configured & reachable
	hResp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	var health struct {
		ImageTranslator imageTranslatorHealth `json:"imageTranslator"`
	}
	_ = json.NewDecoder(hResp.Body).Decode(&health)
	hResp.Body.Close()
	if !health.ImageTranslator.Configured || !health.ImageTranslator.Reachable || health.ImageTranslator.Mode != "builtin:gemini" {
		t.Fatalf("unexpected imageTranslator health: %+v", health.ImageTranslator)
	}

	pagePNG := makeTestMangaPagePNG(t)

	// 2. First POST translates via Vision LLM (cache miss) and returns a valid PNG
	resp1, err := http.Post(srv.URL+"/api/translations/page", "image/png", bytes.NewReader(pagePNG))
	if err != nil {
		t.Fatal(err)
	}
	out1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp1.StatusCode, string(out1))
	}
	if resp1.Header.Get("X-Yata-Cache") != "miss" {
		t.Errorf("expected cache miss, got %q", resp1.Header.Get("X-Yata-Cache"))
	}
	decoded, err := png.Decode(bytes.NewReader(out1))
	if err != nil {
		t.Fatalf("output is not valid png: %v", err)
	}
	if decoded.Bounds().Dx() != 400 || decoded.Bounds().Dy() != 600 {
		t.Errorf("unexpected output dimensions: %v", decoded.Bounds())
	}
	if bytes.Equal(out1, pagePNG) {
		t.Errorf("expected translated page pixels to differ from raw input")
	}

	// 3. Second POST hits disk cache without calling Gemini again
	resp2, err := http.Post(srv.URL+"/api/translations/page", "image/png", bytes.NewReader(pagePNG))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.Header.Get("X-Yata-Cache") != "hit" {
		t.Errorf("expected cache hit on repeat request, got %q", resp2.Header.Get("X-Yata-Cache"))
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 Vision LLM call, got %d", calls.Load())
	}
}

func TestBuiltinVisionPageTranslatorOpenAI(t *testing.T) {
	mockOAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "data:image/jpeg;base64,") {
			t.Errorf("expected openai image_url data URI, got: %s", string(body))
		}
		writeJSON(w, map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "```json\n{\"bubbles\":[{\"box\":[70,110,290,490],\"text\":\"Vamos lá!\"}]}\n```",
					},
				},
			},
		})
	}))
	defer mockOAI.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.TranslateProvider = "openai"
	st.TranslateEndpoint = mockOAI.URL
	st.TranslateAPIKey = "sk-test"
	_ = cfg.save(st)

	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/translations/page", "image/png", bytes.NewReader(makeTestMangaPagePNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(out))
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("output is not valid png: %v", err)
	}
}
