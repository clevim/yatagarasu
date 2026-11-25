package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"createdAt"`
}

func (s shelf) backupsDir() string {
	return filepath.Join(s.dir, "backups")
}

func sanitizeBackupID(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	res := strings.Trim(b.String(), "._")
	if res == "" {
		return "karasu.tachibk"
	}
	if !strings.HasSuffix(res, ".tachibk") && !strings.HasSuffix(res, ".proto.gz") {
		res += ".tachibk"
	}
	return res
}

func (s shelf) validBackupPath(id string) (string, bool) {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", false
	}
	return filepath.Join(s.backupsDir(), id), true
}

func (s shelf) listBackups() ([]BackupInfo, error) {
	dir := s.backupsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]BackupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			ID:        e.Name(),
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s shelf) pruneBackups() {
	maxKeep := s.cfg.get().MaxBackups
	if maxKeep <= 0 {
		maxKeep = 10
	}
	backups, err := s.listBackups()
	if err != nil || len(backups) <= maxKeep {
		return
	}
	for _, b := range backups[maxKeep:] {
		if p, ok := s.validBackupPath(b.ID); ok {
			os.Remove(p)
		}
	}
}

func (s shelf) handleListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.listBackups()
	if err != nil {
		http.Error(w, "list backups failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"backups": backups})
}

func (s shelf) handleUploadBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUpload)
	if err := os.MkdirAll(s.backupsDir(), 0o755); err != nil {
		http.Error(w, "cannot create backups dir", http.StatusInternalServerError)
		return
	}

	var (
		src      io.Reader
		filename string
	)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "bad multipart body", http.StatusBadRequest)
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "bad multipart body", http.StatusBadRequest)
				return
			}
			if p.FormName() == "file" {
				filename = p.FileName()
				src = p
				defer p.Close()
				break
			}
			p.Close()
		}
		if src == nil {
			http.Error(w, "missing file part", http.StatusBadRequest)
			return
		}
	} else {
		filename = r.Header.Get("X-Backup-Filename")
		src = r.Body
	}

	id := fmt.Sprintf("%d_%s", now(), sanitizeBackupID(filename))
	dstPath, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup name", http.StatusBadRequest)
		return
	}

	tmp, err := os.CreateTemp(s.backupsDir(), "backup-*.tmp")
	if err != nil {
		http.Error(w, "cannot store backup", http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	n, err := io.Copy(tmp, src)
	tmp.Close()
	if err != nil || n == 0 {
		os.Remove(tmpPath)
		http.Error(w, "empty or interrupted backup upload", http.StatusBadRequest)
		return
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		http.Error(w, "cannot finalize backup", http.StatusInternalServerError)
		return
	}
	s.pruneBackups()

	writeJSON(w, BackupInfo{
		ID:        id,
		Name:      id,
		Size:      n,
		CreatedAt: now(),
	})
}

func (s shelf) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(p); err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, id))
	http.ServeFile(w, r, p)
}

func (s shelf) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	os.Remove(p)
	w.WriteHeader(http.StatusNoContent)
}

func (s shelf) restoreBackupByID(ctx context.Context, id string) (mangaCount int, suwRestored bool, suwState string, statusCode int, err error) {
	p, ok := s.validBackupPath(id)
	if !ok {
		return 0, false, "", http.StatusBadRequest, errors.New("invalid backup id")
	}
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return 0, false, "", http.StatusNotFound, errors.New("backup not found")
	}
	if err != nil {
		return 0, false, "", http.StatusInternalServerError, errors.New("cannot read backup")
	}

	payload, err := decodeTachibkToSyncPayload(raw)
	if err != nil {
		return 0, false, "", http.StatusBadRequest, fmt.Errorf("invalid .tachibk archive: %w", err)
	}

	serverNowMs := now() * 1000
	syncMu.Lock()
	st := s.loadSyncState()
	mergeSyncIntoState(&st, *payload, serverNowMs)
	saveErr := s.saveSyncState(st)
	syncMu.Unlock()
	if saveErr != nil {
		return 0, false, "", http.StatusInternalServerError, errors.New("failed to persist restored sync state")
	}

	mangaCount = len(payload.Mangas)

	if suw := s.suwayomi(); suw != nil {
		suw.pushCategoriesAndRepos(ctx, payload.Categories, payload.ExtensionRepos)
		restoreID, initStatus, rerr := suw.restoreBackup(ctx, id, bytes.NewReader(raw))
		if rerr != nil {
			return mangaCount, false, "", http.StatusBadGateway, rerr
		}
		finalStatus := suw.waitRestoreBackup(ctx, restoreID, initStatus, 15*time.Second)
		if finalStatus != nil && strings.EqualFold(finalStatus.State, "FAILURE") {
			return mangaCount, false, "FAILURE", http.StatusBadGateway, errors.New("Suwayomi failed to restore backup")
		}
		suwState = "SUCCESS"
		if finalStatus != nil && strings.TrimSpace(finalStatus.State) != "" {
			suwState = finalStatus.State
		}
		return mangaCount, true, suwState, http.StatusOK, nil
	}

	return mangaCount, false, "", http.StatusOK, nil
}

func (s shelf) handleRestoreBackupAPI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mangaCount, suwRestored, suwState, code, err := s.restoreBackupByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, map[string]any{
		"ok":               true,
		"id":               id,
		"importedMangas":   mangaCount,
		"suwayomiRestored": suwRestored,
		"suwayomiState":    suwState,
	})
}

func (s shelf) handleBackupRestoreForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mangaCount, suwRestored, suwState, _, err := s.restoreBackupByID(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/backups?problem="+url.QueryEscape("Restore failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	var note string
	if suwRestored && suwState != "" && !strings.EqualFold(suwState, "SUCCESS") {
		note = fmt.Sprintf("Backup restore in progress (%d manga imported into library; Suwayomi %s).", mangaCount, strings.ToLower(suwState))
	} else {
		note = fmt.Sprintf("Backup restored: %d manga imported into library.", mangaCount)
	}
	http.Redirect(w, r, "/backups?note="+url.QueryEscape(note), http.StatusSeeOther)
}

const maxBackupUpload = 256 << 20 // 256 MiB
