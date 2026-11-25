package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCoverProxySSRFAndDiskCache(t *testing.T) {
	dir := t.TempDir()
	cfg := loadConfig(dir)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. SSRF / bad scheme rejection when coverAllowPrivateIPs is false (default)
	for _, bad := range []string{
		"http://127.0.0.1:4567/secret.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/cover.jpg",
		"file:///etc/passwd",
		"",
	} {
		resp, err := http.Get(srv.URL + "/api/cover?url=" + url.QueryEscape(bad))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("expected SSRF/invalid cover URL %q to be rejected, got 200", bad)
		}
	}

	// 2. Valid image fetch + Referer header + disk cache hit
	var hits atomic.Int32
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(0, 0, color.RGBA{200, 100, 50, 255})
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.Header.Get("Referer"), "http://127.0.0.1:") {
			t.Errorf("expected target-origin Referer header, got %q", r.Header.Get("Referer"))
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBuf.Bytes())
	}))
	defer upstream.Close()

	coverAllowPrivateIPs = true
	defer func() { coverAllowPrivateIPs = false }()

	coverURL := srv.URL + "/api/cover?url=" + url.QueryEscape(upstream.URL+"/cover.png")
	r1, err := http.Get(coverURL)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK || r1.Header.Get("X-Yata-Cache") != "miss" || len(b1) == 0 {
		t.Fatalf("expected 200 cache miss, got %d cache=%q len=%d", r1.StatusCode, r1.Header.Get("X-Yata-Cache"), len(b1))
	}

	r2, err := http.Get(coverURL)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK || r2.Header.Get("X-Yata-Cache") != "hit" {
		t.Fatalf("expected 200 cache hit, got %d cache=%q", r2.StatusCode, r2.Header.Get("X-Yata-Cache"))
	}
	if hits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, got %d", hits.Load())
	}
}

func TestSyncedLibraryCardsClickableAndMaterialize(t *testing.T) {
	var materialized atomic.Bool
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			materialized.Store(true)
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"restoreBackup": map[string]any{
						"id":     "r1",
						"status": map[string]any{"state": "SUCCESS", "mangaProgress": 1, "totalManga": 1},
					},
				},
			})
			return
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "FindMangaID"):
			if materialized.Load() {
				writeJSON(w, map[string]any{"data": map[string]any{"mangas": map[string]any{"nodes": []map[string]any{{"id": 777}}}}})
			} else {
				writeJSON(w, map[string]any{"data": map[string]any{"mangas": map[string]any{"nodes": []map[string]any{}}}})
			}
		case strings.Contains(body.Query, "ExistingLibraryKeys"), strings.Contains(body.Query, "SuwayomiLibrary"):
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}, "mangas": map[string]any{"nodes": []any{}}}})
		default:
			writeJSON(w, map[string]any{"data": map[string]any{}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}

	syncState := s.loadSyncState()
	syncState.Mangas["1234|/manga/berserk"] = SyncManga{
		SourceID:     1234,
		URL:          "/manga/berserk",
		Title:        "Berserk",
		ThumbnailURL: "https://uploads.mangadex.org/covers/abc/cover.jpg",
		Favorite:     true,
		Categories:   []string{"Lendo"},
		Chapters: []SyncChapter{
			{URL: "/ch/1", Name: "Chapter 1", ChapterNumber: 1, Read: true},
			{URL: "/ch/2", Name: "Chapter 2", ChapterNumber: 2, Read: false},
		},
	}
	if err := s.saveSyncState(syncState); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. /library renders clickable <a> card pointing to /manga/sync and proxied cover
	resp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatal(err)
	}
	libHTML, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(libHTML), `href="/manga/sync?sourceId=1234&amp;url=%2Fmanga%2Fberserk"`) {
		t.Fatalf("expected clickable /manga/sync link in /library HTML")
	}
	if !strings.Contains(string(libHTML), `/api/cover?url=https%3A%2F%2Fuploads.mangadex.org`) {
		t.Fatalf("expected proxied /api/cover URL in /library HTML")
	}

	// 2. GET /manga/sync renders synced manga details and chapters
	mResp, err := http.Get(srv.URL + "/manga/sync?sourceId=1234&url=%2Fmanga%2Fberserk")
	if err != nil {
		t.Fatal(err)
	}
	mHTML, _ := io.ReadAll(mResp.Body)
	mResp.Body.Close()
	for _, want := range []string{"Berserk", "Chapter 1", "Chapter 2", "Import into Suwayomi to Read"} {
		if !strings.Contains(string(mHTML), want) {
			t.Errorf("/manga/sync missing %q", want)
		}
	}

	// 3. POST /manga/sync/materialize imports into Suwayomi and redirects to /manga/777
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	pResp, err := noRedir.PostForm(srv.URL+"/manga/sync/materialize", url.Values{
		"sourceId": {"1234"},
		"url":      {"/manga/berserk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pResp.Body.Close()
	if pResp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(pResp.Header.Get("Location"), "/manga/777") {
		t.Fatalf("expected redirect to /manga/777, got %d loc=%q", pResp.StatusCode, pResp.Header.Get("Location"))
	}
}

func TestLegacyExtensionRepoMigratesToExtensionStore(t *testing.T) {
	var addedStores []string
	var fetched atomic.Int32
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "GetExtensionRepos"):
			nodes := make([]map[string]any, 0, len(addedStores))
			for _, u := range addedStores {
				nodes = append(nodes, map[string]any{"indexUrl": u})
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensionStores": map[string]any{"nodes": nodes},
					"settings": map[string]any{
						"extensionRepos": []string{
							"https://github.com/keiyoushi/extensions/raw/repo/index.pb",
							"https://raw.githubusercontent.com/FelipeGFA/extensoes/repo",
						},
					},
				},
			})
		case strings.Contains(body.Query, "AddExtensionStore"):
			if u, ok := body.Variables["url"].(string); ok {
				addedStores = append(addedStores, u)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"addExtensionStore": map[string]any{"extensionStore": map[string]any{"indexUrl": body.Variables["url"]}}}})
		case strings.Contains(body.Query, "FetchExtensions"):
			fetched.Add(1)
			writeJSON(w, map[string]any{"data": map[string]any{"fetchExtensions": map[string]any{"extensions": []any{}}}})
		case strings.Contains(body.Query, "ListExtensions"):
			if len(addedStores) >= 2 && fetched.Load() > 0 {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"extensions": map[string]any{
							"nodes": []map[string]any{
								{"pkgName": "eu.kanade.tachiyomi.extension.all.mangadex", "name": "MangaDex", "lang": "all", "versionName": "1.4.0", "isInstalled": false},
							},
						},
					},
				})
			} else {
				writeJSON(w, map[string]any{"data": map[string]any{"extensions": map[string]any{"nodes": []any{}}}})
			}
		default:
			writeJSON(w, map[string]any{"data": map[string]any{}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/browse?tab=extensions")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(html), "MangaDex") {
		t.Fatalf("expected migrated ExtensionStore to populate MangaDex on /browse?tab=extensions")
	}
	if len(addedStores) != 2 {
		t.Fatalf("expected both legacy repos (.pb and bare) to migrate into ExtensionStores, got %v", addedStores)
	}
}

func TestEntrypointHardensTmpAndHome(t *testing.T) {
	b, err := os.ReadFile("entrypoint-allinone.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"export TMPDIR=", "export HOME=", "-Djava.io.tmpdir=", "-Duser.home="} {
		if !strings.Contains(src, want) {
			t.Errorf("entrypoint-allinone.sh missing %q", want)
		}
	}
	if got := cleanGraphQLError("Exception: /tmp read-only\r\n\tat java.base/Foo.bar(Foo.java:1)"); got != "Exception: /tmp read-only" {
		t.Errorf("cleanGraphQLError failed: got %q", got)
	}
}
