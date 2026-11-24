package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// imageTranslatorClient talks to the optional zyddnys/manga-image-translator
// sidecar (`docker compose --profile translator up -d`), which performs full
// server-side text detection -> OCR -> translation -> LaMa inpainting -> typesetting.
type imageTranslatorClient struct {
	baseURL string
	lang    string
	http    *http.Client
}

type imageTranslatorHealth struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	TargetLang string `json:"targetLang,omitempty"`
}

func (s shelf) imageTranslator() *imageTranslatorClient {
	cur := s.cfg.get()
	if cur.ImageTranslatorURL == "" {
		return nil
	}
	lang := cur.ImageTranslatorLang
	if lang == "" {
		lang = "PTB"
	}
	return &imageTranslatorClient{
		baseURL: trimSlash(cur.ImageTranslatorURL),
		lang:    lang,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *imageTranslatorClient) checkHealth(ctx context.Context) imageTranslatorHealth {
	h := imageTranslatorHealth{Configured: true, TargetLang: c.lang}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return h
	}
	probe := &http.Client{Timeout: 2500 * time.Millisecond}
	resp, err := probe.Do(req)
	if err == nil {
		resp.Body.Close()
		h.Reachable = resp.StatusCode >= 200 && resp.StatusCode < 400
	}
	return h
}

func (c *imageTranslatorClient) translateImagePNG(ctx context.Context, imgBytes []byte, targetLang string) ([]byte, error) {
	if targetLang == "" {
		targetLang = c.lang
	}
	cfgJSON, _ := json.Marshal(map[string]any{
		"detector": map[string]any{
			"detector":       "default",
			"detection_size": 1536,
		},
		"inpainter": map[string]any{
			"inpainter":       "lama_mpe",
			"inpainting_size": 1536,
		},
		"translator": map[string]any{
			"translator":  "sugoi",
			"target_lang": targetLang,
		},
	})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "page.png")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(imgBytes); err != nil {
		return nil, err
	}
	if err := mw.WriteField("config", string(cfgJSON)); err != nil {
		return nil, err
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/translate/with-form/image", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("manga-image-translator http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func (s shelf) renderedPagePath(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(s.translationsDir(), "rendered", hex.EncodeToString(h[:16])+".png")
}

// handleTranslatePageImage accepts a raw image upload (or multipart `image`)
// and returns the inpainted + typeset PNG from zyddnys/manga-image-translator,
// cached on disk by content hash + target language so repeated views are instant.
func (s shelf) handleTranslatePageImage(w http.ResponseWriter, r *http.Request) {
	it := s.imageTranslator()
	if it == nil {
		http.Error(w, "image translator sidecar is not configured (set YATA_IMAGE_TRANSLATOR_URL)", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)

	var imgBytes []byte
	var err error
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		file, _, ferr := r.FormFile("image")
		if ferr != nil {
			http.Error(w, "missing image form field", http.StatusBadRequest)
			return
		}
		defer file.Close()
		imgBytes, err = io.ReadAll(io.LimitReader(file, 32<<20))
	} else {
		imgBytes, err = io.ReadAll(io.LimitReader(r.Body, 32<<20))
	}
	if err != nil || len(imgBytes) == 0 {
		http.Error(w, "empty image payload", http.StatusBadRequest)
		return
	}

	targetLang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if targetLang == "" {
		targetLang = it.lang
	}

	sum := sha256.Sum256(imgBytes)
	cachePath := s.renderedPagePath(hex.EncodeToString(sum[:]) + "|" + targetLang)
	if cached, err := os.ReadFile(cachePath); err == nil && len(cached) > 0 {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Yata-Cache", "hit")
		w.Write(cached)
		return
	}

	pngBytes, err := it.translateImagePNG(r.Context(), imgBytes, targetLang)
	if err != nil {
		http.Error(w, "image translation failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		tmp := cachePath + ".tmp"
		if os.WriteFile(tmp, pngBytes, 0o644) == nil {
			_ = os.Rename(tmp, cachePath)
		}
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Yata-Cache", "miss")
	w.Write(pngBytes)
}
