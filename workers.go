package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// translateAndCachePagePNG translates a single manga page image via the
// configured manga-image-translator sidecar, reusing and atomically populating
// the shared rendered PNG cache under <data>/translations/rendered/.
func (s shelf) translateAndCachePagePNG(ctx context.Context, it *imageTranslatorClient, imgBytes []byte, targetLang string) ([]byte, bool, error) {
	if it == nil {
		return nil, false, errors.New("image translator sidecar is not configured")
	}
	if len(imgBytes) == 0 {
		return nil, false, errors.New("empty image payload")
	}
	if targetLang == "" {
		targetLang = it.lang
	}
	sum := sha256.Sum256(imgBytes)
	cachePath := s.renderedPagePath(hex.EncodeToString(sum[:]) + "|" + targetLang)
	if cached, err := os.ReadFile(cachePath); err == nil && len(cached) > 0 {
		return cached, true, nil
	}

	pngBytes, err := it.translateImagePNG(ctx, imgBytes, targetLang)
	if err != nil {
		return nil, false, err
	}
	if len(pngBytes) == 0 {
		return nil, false, errors.New("image translator returned empty response")
	}

	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		if tmp, err := os.CreateTemp(cacheDir, "page-*.png.tmp"); err == nil {
			tmpPath := tmp.Name()
			if _, werr := tmp.Write(pngBytes); werr == nil && tmp.Close() == nil {
				if os.Rename(tmpPath, cachePath) != nil {
					_ = os.Remove(tmpPath)
				}
			} else {
				_ = tmp.Close()
				_ = os.Remove(tmpPath)
			}
		}
	}
	return pngBytes, false, nil
}

func sanitizeLangCode(lang string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(lang) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

// ---- A2: KOReader Translated CBZ Burn-in ------------------------------------

// buildTranslatedCBZ extracts pages from srcCBZPath, translates each image via
// manga-image-translator (reusing the per-page disk cache), and writes a cached
// CBZ atomically under <data>/translations/cbz/<chapterId>_<lang>.cbz.
func (s shelf) buildTranslatedCBZ(ctx context.Context, chapterID int64, srcCBZPath, targetLang string) (string, error) {
	it := s.imageTranslator()
	if it == nil {
		return "", errors.New("image translator sidecar is not configured")
	}
	if targetLang == "" {
		targetLang = it.lang
	}
	srcSt, err := os.Stat(srcCBZPath)
	if err != nil {
		return "", err
	}

	cbzDir := filepath.Join(s.translationsDir(), "cbz")
	langSafe := sanitizeLangCode(targetLang)
	dstPath := filepath.Join(cbzDir, fmt.Sprintf("%d_%s.cbz", chapterID, langSafe))

	// Reuse cached CBZ if present and at least as fresh as the source archive.
	if dstSt, err := os.Stat(dstPath); err == nil && dstSt.Size() > 0 && !dstSt.ModTime().Before(srcSt.ModTime()) {
		return dstPath, nil
	}

	zr, err := zip.OpenReader(srcCBZPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	if err := os.MkdirAll(cbzDir, 0o755); err != nil {
		return "", err
	}
	tmpFile, err := os.CreateTemp(cbzDir, fmt.Sprintf("%d_%s-*.cbz.tmp", chapterID, langSafe))
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	zw := zip.NewWriter(tmpFile)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = zw.Close()
			_ = tmpFile.Close()
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			_ = zw.Close()
			_ = tmpFile.Close()
			return "", err
		}
		raw, rerr := io.ReadAll(io.LimitReader(rc, 32<<20))
		_ = rc.Close()
		if rerr != nil {
			_ = zw.Close()
			_ = tmpFile.Close()
			return "", rerr
		}

		outName := f.Name
		outBytes := raw
		method := zip.Deflate

		if isImageName(f.Name) {
			pngBytes, _, terr := s.translateAndCachePagePNG(ctx, it, raw, targetLang)
			if terr != nil {
				_ = zw.Close()
				_ = tmpFile.Close()
				return "", fmt.Errorf("page %s: %w", f.Name, terr)
			}
			ext := path.Ext(f.Name)
			outName = strings.TrimSuffix(f.Name, ext) + ".png"
			outBytes = pngBytes
			method = zip.Store // PNGs are already compressed
		}

		hdr := &zip.FileHeader{
			Name:     outName,
			Method:   method,
			Modified: time.Now(),
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			_ = tmpFile.Close()
			return "", err
		}
		if _, err := w.Write(outBytes); err != nil {
			_ = zw.Close()
			_ = tmpFile.Close()
			return "", err
		}
	}

	if err := zw.Close(); err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", err
	}
	cleanupTmp = false
	return dstPath, nil
}

// ---- A3: Background Pre-Translation Queue -----------------------------------

type PreTranslateJob struct {
	Source    string `json:"source"` // "suwayomi" | "shelf"
	ChapterID int64  `json:"chapterId"`
	Title     string `json:"title"`
}

type PreTranslateRecord struct {
	Source    string `json:"source"`
	ChapterID int64  `json:"chapterId"`
	Title     string `json:"title"`
	Pages     int    `json:"pages"`
	Status    string `json:"status"` // "done" | "error"
	Error     string `json:"error,omitempty"`
	Finished  string `json:"finished"`
}

type PreTranslateStatus struct {
	Pending   int                  `json:"pending"`
	Active    *PreTranslateJob     `json:"active,omitempty"`
	Completed int                  `json:"completed"`
	Errors    int                  `json:"errors"`
	Recent    []PreTranslateRecord `json:"recent,omitempty"`
}

type preTranslateQueue struct {
	mu           sync.Mutex
	ch           chan PreTranslateJob
	once         sync.Once
	s            shelf
	inFlight     map[string]bool
	pendingCount int
	active       *PreTranslateJob
	completed    int
	errors       int
	recent       []PreTranslateRecord
}

var preTranslateRegistry = struct {
	mu    sync.Mutex
	byDir map[string]*preTranslateQueue
}{
	byDir: map[string]*preTranslateQueue{},
}

func (s shelf) preTranslateQueue() *preTranslateQueue {
	preTranslateRegistry.mu.Lock()
	defer preTranslateRegistry.mu.Unlock()
	q, ok := preTranslateRegistry.byDir[s.dir]
	if !ok {
		q = &preTranslateQueue{
			ch:       make(chan PreTranslateJob, 64),
			s:        s,
			inFlight: map[string]bool{},
		}
		preTranslateRegistry.byDir[s.dir] = q
	}
	return q
}

func preTranslateKey(source string, chapterID int64) string {
	return fmt.Sprintf("%s:%d", source, chapterID)
}

func (q *preTranslateQueue) enqueue(source string, chapterID int64, title string) bool {
	if source != "suwayomi" && source != "shelf" {
		return false
	}
	if chapterID <= 0 {
		return false
	}
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("%s #%d", source, chapterID)
	}
	q.once.Do(func() {
		go q.run()
	})

	key := preTranslateKey(source, chapterID)
	q.mu.Lock()
	if q.inFlight[key] {
		q.mu.Unlock()
		return false
	}
	job := PreTranslateJob{
		Source:    source,
		ChapterID: chapterID,
		Title:     title,
	}
	select {
	case q.ch <- job:
		q.inFlight[key] = true
		q.pendingCount++
		q.mu.Unlock()
		return true
	default:
		q.mu.Unlock()
		return false
	}
}

func (q *preTranslateQueue) status() PreTranslateStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	st := PreTranslateStatus{
		Pending:   q.pendingCount,
		Completed: q.completed,
		Errors:    q.errors,
	}
	if q.active != nil {
		cp := *q.active
		st.Active = &cp
	}
	if len(q.recent) > 0 {
		st.Recent = make([]PreTranslateRecord, len(q.recent))
		copy(st.Recent, q.recent)
	}
	return st
}

func (q *preTranslateQueue) run() {
	for job := range q.ch {
		q.mu.Lock()
		cp := job
		q.active = &cp
		if q.pendingCount > 0 {
			q.pendingCount--
		}
		q.mu.Unlock()

		pages, err := q.process(job)

		rec := PreTranslateRecord{
			Source:    job.Source,
			ChapterID: job.ChapterID,
			Title:     job.Title,
			Pages:     pages,
			Status:    "done",
			Finished:  humanTime(now()),
		}
		if err != nil {
			rec.Status = "error"
			rec.Error = err.Error()
			log.Printf("yata: pre-translate failed (%s #%d): %v", job.Source, job.ChapterID, err)
		}

		key := preTranslateKey(job.Source, job.ChapterID)
		q.mu.Lock()
		q.active = nil
		delete(q.inFlight, key)
		if err != nil {
			q.errors++
		} else {
			q.completed++
		}
		q.recent = append([]PreTranslateRecord{rec}, q.recent...)
		if len(q.recent) > 20 {
			q.recent = q.recent[:20]
		}
		q.mu.Unlock()
	}
}

func (q *preTranslateQueue) process(job PreTranslateJob) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	it := q.s.imageTranslator()
	if it == nil {
		return 0, errors.New("image translator sidecar is not configured")
	}

	switch job.Source {
	case "suwayomi":
		suw := q.s.suwayomi()
		if suw == nil {
			return 0, errors.New("suwayomi is not configured")
		}
		pages, err := suw.getChapterPages(ctx, job.ChapterID)
		if err != nil {
			return 0, err
		}
		for i, p := range pages {
			imgPath := p
			if u, perr := url.Parse(p); perr == nil && u.Path == "/api/suwayomi/image" {
				if qp := u.Query().Get("path"); qp != "" {
					imgPath = qp
				}
			}
			raw, err := suw.fetchImageBytes(ctx, imgPath)
			if err != nil {
				return i, fmt.Errorf("fetch page %d: %w", i+1, err)
			}
			if _, _, err := q.s.translateAndCachePagePNG(ctx, it, raw, it.lang); err != nil {
				return i, fmt.Errorf("translate page %d: %w", i+1, err)
			}
		}
		return len(pages), nil

	case "shelf":
		names, err := listCBZImages(q.s.cbz(job.ChapterID))
		if err != nil {
			return 0, err
		}
		for i := range names {
			raw, err := q.s.readShelfChapterPageBytes(job.ChapterID, i)
			if err != nil {
				return i, fmt.Errorf("read page %d: %w", i+1, err)
			}
			if _, _, err := q.s.translateAndCachePagePNG(ctx, it, raw, it.lang); err != nil {
				return i, fmt.Errorf("translate page %d: %w", i+1, err)
			}
		}
		return len(names), nil

	default:
		return 0, fmt.Errorf("unknown source %q", job.Source)
	}
}

// ---- C4: Auto-Delete Read Downloads -----------------------------------------

// cleanupReadDownloads lists downloaded chapters marked read in Suwayomi,
// keeps the newest KeepReadChapters per manga (sorted by ChapterNumber DESC),
// and deletes the rest via deleteDownloadedChapters.
func (s shelf) cleanupReadDownloads(ctx context.Context) (int, error) {
	suw := s.suwayomi()
	if suw == nil {
		return 0, errors.New("suwayomi not configured")
	}
	chapters, err := suw.listDownloadedReadChapters(ctx)
	if err != nil {
		return 0, err
	}
	keep := s.cfg.get().KeepReadChapters
	if keep < 0 {
		keep = 0
	}

	byManga := map[int64][]SuwayomiDownloadedChapter{}
	for _, ch := range chapters {
		byManga[ch.MangaID] = append(byManga[ch.MangaID], ch)
	}

	var toDelete []int64
	for _, list := range byManga {
		sort.Slice(list, func(i, j int) bool {
			if list[i].ChapterNumber != list[j].ChapterNumber {
				return list[i].ChapterNumber > list[j].ChapterNumber
			}
			if list[i].SourceOrder != list[j].SourceOrder {
				return list[i].SourceOrder > list[j].SourceOrder
			}
			return list[i].ID > list[j].ID
		})
		if len(list) > keep {
			for _, ch := range list[keep:] {
				toDelete = append(toDelete, ch.ID)
			}
		}
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	sort.Slice(toDelete, func(i, j int) bool { return toDelete[i] < toDelete[j] })
	if err := suw.deleteDownloadedChapters(ctx, toDelete); err != nil {
		return 0, err
	}
	return len(toDelete), nil
}
