package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mirrors karasu.translation.model.PageTranslation / TranslationBlock / BalloonBox / SeriesNotes.
type BalloonBox struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

type TranslationBlock struct {
	Text        string      `json:"text"`
	Translation string      `json:"translation"`
	Width       float32     `json:"width"`
	Height      float32     `json:"height"`
	X           float32     `json:"x"`
	Y           float32     `json:"y"`
	SymWidth    float32     `json:"symWidth"`
	SymHeight   float32     `json:"symHeight"`
	Angle       float32     `json:"angle"`
	Background  int         `json:"background,omitempty"`
	Balloon     *BalloonBox `json:"balloon,omitempty"`
}

type PageTranslation struct {
	Blocks    []TranslationBlock `json:"blocks"`
	ImgWidth  float32            `json:"imgWidth"`
	ImgHeight float32            `json:"imgHeight"`
}

type SeriesNotes struct {
	Notes    string            `json:"notes,omitempty"`
	Glossary map[string]string `json:"glossary,omitempty"`
}

type TranslateRequest struct {
	FromLang    string                     `json:"fromLang"`
	ToLang      string                     `json:"toLang"`
	SeriesNotes SeriesNotes                `json:"seriesNotes"`
	Pages       map[string]PageTranslation `json:"pages"`
}

type TranslateResponse struct {
	Pages   map[string]PageTranslation `json:"pages"`
	Learned map[string]string          `json:"learned,omitempty"`
}

type translateHealth struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

func (s shelf) translateStatus() translateHealth {
	cur := s.cfg.get()
	if cur.TranslateProvider == "" {
		return translateHealth{Configured: false}
	}
	return translateHealth{
		Configured: true,
		Provider:   cur.TranslateProvider,
		Model:      cur.TranslateModel,
	}
}

func (s shelf) translationsDir() string {
	return filepath.Join(s.dir, "translations")
}

func (s shelf) chapterTranslationPath(sourceID int64, mangaURL, chapterURL, toLang string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", sourceID, mangaURL, chapterURL, toLang)))
	return filepath.Join(s.translationsDir(), "chapters", hex.EncodeToString(h[:16])+".json")
}

func (s shelf) contextTranslationPath(sourceID int64, mangaURL string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", sourceID, mangaURL)))
	return filepath.Join(s.translationsDir(), "context", hex.EncodeToString(h[:16])+".json")
}

var transCacheMu sync.Mutex

func (s shelf) transCachePath() string {
	return filepath.Join(s.translationsDir(), "cache.json")
}

func (s shelf) loadTransCache() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(s.transCachePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func (s shelf) saveTransCache(cache map[string]string) {
	_ = os.MkdirAll(s.translationsDir(), 0o755)
	b, err := json.Marshal(cache)
	if err != nil {
		return
	}
	tmp := s.transCachePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.transCachePath())
	}
}

func (s shelf) handleGetChapterTranslation(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	chapterURL := r.URL.Query().Get("chapterUrl")
	toLang := r.URL.Query().Get("toLang")
	if err != nil || mangaURL == "" || chapterURL == "" || toLang == "" {
		http.Error(w, "sourceId, mangaUrl, chapterUrl and toLang are required", http.StatusBadRequest)
		return
	}
	p := s.chapterTranslationPath(sourceID, mangaURL, chapterURL, toLang)
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, "translation not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func (s shelf) handlePutChapterTranslation(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	chapterURL := r.URL.Query().Get("chapterUrl")
	toLang := r.URL.Query().Get("toLang")
	if err != nil || mangaURL == "" || chapterURL == "" || toLang == "" {
		http.Error(w, "sourceId, mangaUrl, chapterUrl and toLang are required", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil || !json.Valid(body) {
		http.Error(w, "invalid translation json", http.StatusBadRequest)
		return
	}
	p := s.chapterTranslationPath(sourceID, mangaURL, chapterURL, toLang)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		http.Error(w, "cannot store translation", http.StatusInternalServerError)
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		http.Error(w, "cannot store translation", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		http.Error(w, "cannot finalize translation", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s shelf) handleGetTranslationContext(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	if err != nil || mangaURL == "" {
		http.Error(w, "sourceId and mangaUrl are required", http.StatusBadRequest)
		return
	}
	b, err := os.ReadFile(s.contextTranslationPath(sourceID, mangaURL))
	if err != nil {
		writeJSON(w, SeriesNotes{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func (s shelf) handlePutTranslationContext(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	if err != nil || mangaURL == "" {
		http.Error(w, "sourceId and mangaUrl are required", http.StatusBadRequest)
		return
	}
	var notes SeriesNotes
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&notes); err != nil {
		http.Error(w, "invalid series notes json", http.StatusBadRequest)
		return
	}
	p := s.contextTranslationPath(sourceID, mangaURL)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		http.Error(w, "cannot store context", http.StatusInternalServerError)
		return
	}
	b, _ := json.Marshal(notes)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil || os.Rename(tmp, p) != nil {
		http.Error(w, "cannot store context", http.StatusInternalServerError)
		return
	}
	writeJSON(w, notes)
}

func (s shelf) handleTranslate(w http.ResponseWriter, r *http.Request) {
	var req TranslateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid translate request", http.StatusBadRequest)
		return
	}
	if req.ToLang == "" {
		http.Error(w, "toLang is required", http.StatusBadRequest)
		return
	}

	cur := s.cfg.get()
	cacheScope := fmt.Sprintf("%s:%s|%s|%s", cur.TranslateProvider, cur.TranslateModel, req.FromLang, req.ToLang)

	type pendingRef struct {
		pageKey  string
		blockIdx int
		text     string
	}
	var pending []pendingRef

	transCacheMu.Lock()
	cache := s.loadTransCache()
	transCacheMu.Unlock()

	pageKeys := make([]string, 0, len(req.Pages))
	for k := range req.Pages {
		pageKeys = append(pageKeys, k)
	}
	sort.Strings(pageKeys)

	for _, pk := range pageKeys {
		pt := req.Pages[pk]
		for i := range pt.Blocks {
			if strings.TrimSpace(pt.Blocks[i].Translation) != "" {
				continue
			}
			src := strings.TrimSpace(pt.Blocks[i].Text)
			if src == "" {
				continue
			}
			if cached, ok := cache[cacheScope+"|"+src]; ok && cached != "" {
				pt.Blocks[i].Translation = cached
				continue
			}
			pending = append(pending, pendingRef{pageKey: pk, blockIdx: i, text: src})
		}
		req.Pages[pk] = pt
	}

	learned := map[string]string{}
	if len(pending) > 0 {
		if cur.TranslateProvider == "" {
			http.Error(w, "server-side translation is not configured on yatagarasu", http.StatusServiceUnavailable)
			return
		}
		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.text
		}
		translated, glossary, err := callTranslateLLM(r.Context(), cur, req.FromLang, req.ToLang, req.SeriesNotes, texts)
		if err != nil {
			http.Error(w, "translation failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		learned = glossary

		transCacheMu.Lock()
		cache = s.loadTransCache()
		for i, ref := range pending {
			if i < len(translated) && strings.TrimSpace(translated[i]) != "" {
				pt := req.Pages[ref.pageKey]
				pt.Blocks[ref.blockIdx].Translation = translated[i]
				req.Pages[ref.pageKey] = pt
				cache[cacheScope+"|"+ref.text] = translated[i]
			}
		}
		s.saveTransCache(cache)
		transCacheMu.Unlock()
	}

	writeJSON(w, TranslateResponse{
		Pages:   req.Pages,
		Learned: learned,
	})
}

func callTranslateLLM(ctx context.Context, cur settings, fromLang, toLang string, notes SeriesNotes, lines []string) ([]string, map[string]string, error) {
	systemPrompt := fmt.Sprintf(
		"You are a manga translator. Translate the numbered speech bubbles from %s to %s. "+
			"Keep natural dialogue tone and concise phrasing that fits comic bubbles. "+
			"Return ONLY a JSON object: {\"translations\": [\"...\"], \"glossary\": {\"orig\": \"translated\"}} "+
			"with the exact same number of entries in \"translations\" as the input array.",
		fromLang, toLang,
	)
	if notes.Notes != "" || len(notes.Glossary) > 0 {
		nb, _ := json.Marshal(notes)
		systemPrompt += " Series context: " + string(nb)
	}
	userBytes, _ := json.Marshal(lines)

	client := &http.Client{Timeout: 120 * time.Second}
	var rawContent string
	var err error

	switch cur.TranslateProvider {
	case "gemini":
		rawContent, err = callGemini(ctx, client, cur, systemPrompt, string(userBytes))
	case "openai":
		rawContent, err = callOpenAICompat(ctx, client, cur, systemPrompt, string(userBytes))
	default:
		return nil, nil, fmt.Errorf("unsupported translate provider %q", cur.TranslateProvider)
	}
	if err != nil {
		return nil, nil, err
	}

	cleaned := stripJSONFences(rawContent)
	var parsed struct {
		Translations []string          `json:"translations"`
		Glossary     map[string]string `json:"glossary"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, nil, fmt.Errorf("llm returned invalid json: %w", err)
	}
	return parsed.Translations, parsed.Glossary, nil
}

func callOpenAICompat(ctx context.Context, client *http.Client, cur settings, sys, user string) (string, error) {
	endpoint := cur.TranslateEndpoint
	if endpoint == "" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}
	model := cur.TranslateModel
	if model == "" {
		model = "google/gemma-4-31b-it:free"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature": 0.2,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cur.TranslateAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cur.TranslateAPIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("empty choices from openai-compatible provider")
	}
	return out.Choices[0].Message.Content, nil
}

func callGemini(ctx context.Context, client *http.Client, cur settings, sys, user string) (string, error) {
	model := cur.TranslateModel
	if model == "" {
		model = "gemini-2.5-flash"
	}
	endpoint := cur.TranslateEndpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	}
	body, _ := json.Marshal(map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": sys}},
		},
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": user}}},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"temperature":      0.2,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cur.TranslateAPIKey != "" {
		req.Header.Set("x-goog-api-key", cur.TranslateAPIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("gemini http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("empty response from gemini")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
