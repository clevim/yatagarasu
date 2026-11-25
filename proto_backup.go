package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const maxDecompressedTachibk = 256 << 20 // 256 MiB safety cap

func appendProtoTag(dst []byte, fieldNum, wireType uint64) []byte {
	return binary.AppendUvarint(dst, (fieldNum<<3)|wireType)
}

func appendProtoVarint(dst []byte, fieldNum, v uint64) []byte {
	dst = appendProtoTag(dst, fieldNum, 0)
	return binary.AppendUvarint(dst, v)
}

func appendProtoBytes(dst []byte, fieldNum uint64, b []byte) []byte {
	dst = appendProtoTag(dst, fieldNum, 2)
	dst = binary.AppendUvarint(dst, uint64(len(b)))
	return append(dst, b...)
}

func appendProtoString(dst []byte, fieldNum uint64, s string) []byte {
	dst = appendProtoTag(dst, fieldNum, 2)
	dst = binary.AppendUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}

func appendProtoFixed32Float(dst []byte, fieldNum uint64, f float32) []byte {
	dst = appendProtoTag(dst, fieldNum, 5)
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(f))
}

func boolToUint64(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

func readProtoTag(b []byte) (fieldNum, wireType uint64, n int, err error) {
	tag, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, 0, errors.New("invalid protobuf tag")
	}
	fieldNum = tag >> 3
	wireType = tag & 0x7
	if fieldNum == 0 {
		return 0, 0, 0, errors.New("invalid protobuf field number 0")
	}
	return fieldNum, wireType, n, nil
}

func readProtoVarint(b []byte) (uint64, int, error) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, errors.New("invalid protobuf varint")
	}
	return v, n, nil
}

func readProtoBytes(b []byte) ([]byte, int, error) {
	length, n := binary.Uvarint(b)
	if n <= 0 {
		return nil, 0, errors.New("invalid protobuf length")
	}
	rem := b[n:]
	if length > uint64(len(rem)) {
		return nil, 0, errors.New("truncated protobuf length-delimited field")
	}
	end := n + int(length)
	return b[n:end], end, nil
}

func readProtoFixed32(b []byte) (uint32, int, error) {
	if len(b) < 4 {
		return 0, 0, errors.New("truncated protobuf fixed32")
	}
	return binary.LittleEndian.Uint32(b[:4]), 4, nil
}

func skipProtoValue(b []byte, wireType uint64) (int, error) {
	switch wireType {
	case 0:
		_, n, err := readProtoVarint(b)
		return n, err
	case 1:
		if len(b) < 8 {
			return 0, errors.New("truncated protobuf fixed64")
		}
		return 8, nil
	case 2:
		_, n, err := readProtoBytes(b)
		return n, err
	case 5:
		if len(b) < 4 {
			return 0, errors.New("truncated protobuf fixed32")
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported protobuf wire type %d", wireType)
	}
}

func cleanFloat32ToFloat64(f32 float32) float64 {
	f64 := float64(f32)
	if math.IsNaN(f64) || math.IsInf(f64, 0) {
		return 0
	}
	if parsed, err := strconv.ParseFloat(strconv.FormatFloat(f64, 'g', -1, 32), 64); err == nil {
		return parsed
	}
	return f64
}

// encodeSyncPayloadToTachibk encodes categories, mangas (with chapters), and optional
// extension repositories into a gzipped Tachiyomi / Mihon / Karasu .tachibk protobuf archive.
func encodeSyncPayloadToTachibk(cats []SyncCategory, mangas []SyncManga, extensionRepos ...string) ([]byte, error) {
	// Build a complete, deduplicated category list with unique non-negative orders.
	var orderedCats []SyncCategory
	seenCat := make(map[string]int, len(cats))
	for _, c := range cats {
		name := strings.TrimSpace(c.Name)
		if name == "" || strings.EqualFold(name, "Default") {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seenCat[key]; exists {
			continue
		}
		c.Name = name
		seenCat[key] = len(orderedCats)
		orderedCats = append(orderedCats, c)
	}
	for _, m := range mangas {
		for _, rawName := range m.Categories {
			name := strings.TrimSpace(rawName)
			if name == "" || strings.EqualFold(name, "Default") {
				continue
			}
			key := strings.ToLower(name)
			if _, exists := seenCat[key]; !exists {
				seenCat[key] = len(orderedCats)
				orderedCats = append(orderedCats, SyncCategory{Name: name, Order: -1})
			}
		}
	}

	usedOrders := make(map[int]bool, len(orderedCats))
	needOrder := make([]int, 0)
	for i := range orderedCats {
		ord := orderedCats[i].Order
		if ord >= 0 && !usedOrders[ord] {
			usedOrders[ord] = true
		} else {
			needOrder = append(needOrder, i)
		}
	}
	nextOrd := 0
	for _, idx := range needOrder {
		for usedOrders[nextOrd] {
			nextOrd++
		}
		orderedCats[idx].Order = nextOrd
		usedOrders[nextOrd] = true
		nextOrd++
	}

	catOrderByLower := make(map[string]int, len(orderedCats))
	for _, c := range orderedCats {
		catOrderByLower[strings.ToLower(c.Name)] = c.Order
	}

	var root []byte

	// Field 1: repeated BackupManga
	for _, m := range mangas {
		var mb []byte
		mb = appendProtoVarint(mb, 1, uint64(m.SourceID))
		mb = appendProtoString(mb, 2, m.URL)
		mb = appendProtoString(mb, 3, m.Title)
		if m.ThumbnailURL != "" {
			mb = appendProtoString(mb, 9, m.ThumbnailURL)
		}
		mb = appendProtoVarint(mb, 13, uint64(m.UpdatedAt))

		for idx, ch := range m.Chapters {
			var cb []byte
			cb = appendProtoString(cb, 1, ch.URL)
			cb = appendProtoString(cb, 2, ch.Name)
			if ch.Scanlator != "" {
				cb = appendProtoString(cb, 3, ch.Scanlator)
			}
			cb = appendProtoVarint(cb, 4, boolToUint64(ch.Read))
			cb = appendProtoVarint(cb, 5, boolToUint64(ch.Bookmark))
			cb = appendProtoVarint(cb, 6, uint64(ch.LastPageRead))
			cb = appendProtoVarint(cb, 7, uint64(ch.UpdatedAt))
			cb = appendProtoVarint(cb, 8, uint64(ch.UpdatedAt))
			cb = appendProtoFixed32Float(cb, 9, float32(ch.ChapterNumber))
			cb = appendProtoVarint(cb, 10, uint64(idx))
			mb = appendProtoBytes(mb, 16, cb)
		}

		for _, cname := range m.Categories {
			if ord, ok := catOrderByLower[strings.ToLower(strings.TrimSpace(cname))]; ok {
				// kotlinx.serialization.protobuf without @ProtoPacked encodes List<Int> as unpacked varints.
				mb = appendProtoVarint(mb, 17, uint64(ord))
			}
		}

		mb = appendProtoVarint(mb, 100, boolToUint64(m.Favorite))
		root = appendProtoBytes(root, 1, mb)
	}

	// Field 2: repeated BackupCategory
	for _, c := range orderedCats {
		var catB []byte
		catB = appendProtoString(catB, 1, c.Name)
		catB = appendProtoVarint(catB, 2, uint64(c.Order))
		catB = appendProtoVarint(catB, 100, uint64(c.Flags))
		root = appendProtoBytes(root, 2, catB)
	}

	// Field 106: repeated BackupExtensionRepo
	seenRepo := map[string]bool{}
	for _, rawRepo := range extensionRepos {
		r := strings.TrimSpace(rawRepo)
		if r == "" || seenRepo[r] {
			continue
		}
		seenRepo[r] = true
		var rb []byte
		rb = appendProtoString(rb, 1, r)
		rb = appendProtoString(rb, 2, r)
		root = appendProtoBytes(root, 106, rb)
	}

	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	if _, err := gw.Write(root); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type rawDecodedManga struct {
	manga  SyncManga
	catIDs []int
}

func decodeBackupCategory(b []byte) (SyncCategory, error) {
	var cat SyncCategory
	for len(b) > 0 {
		fieldNum, wireType, n, err := readProtoTag(b)
		if err != nil {
			return cat, err
		}
		b = b[n:]
		switch {
		case fieldNum == 1 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return cat, err
			}
			cat.Name = string(sb)
			b = b[vn:]
		case fieldNum == 2 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return cat, err
			}
			cat.Order = int(int64(v))
			b = b[vn:]
		case fieldNum == 100 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return cat, err
			}
			cat.Flags = int64(v)
			b = b[vn:]
		default:
			vn, err := skipProtoValue(b, wireType)
			if err != nil {
				return cat, err
			}
			b = b[vn:]
		}
	}
	return cat, nil
}

func decodeBackupChapter(b []byte) (SyncChapter, error) {
	var (
		ch         SyncChapter
		dateFetch  int64
		dateUpload int64
	)
	for len(b) > 0 {
		fieldNum, wireType, n, err := readProtoTag(b)
		if err != nil {
			return ch, err
		}
		b = b[n:]
		switch {
		case fieldNum == 1 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return ch, err
			}
			ch.URL = string(sb)
			b = b[vn:]
		case fieldNum == 2 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return ch, err
			}
			ch.Name = string(sb)
			b = b[vn:]
		case fieldNum == 3 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return ch, err
			}
			ch.Scanlator = string(sb)
			b = b[vn:]
		case fieldNum == 4 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return ch, err
			}
			ch.Read = v != 0
			b = b[vn:]
		case fieldNum == 5 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return ch, err
			}
			ch.Bookmark = v != 0
			b = b[vn:]
		case fieldNum == 6 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return ch, err
			}
			ch.LastPageRead = int(int64(v))
			b = b[vn:]
		case fieldNum == 7 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return ch, err
			}
			dateFetch = int64(v)
			b = b[vn:]
		case fieldNum == 8 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return ch, err
			}
			dateUpload = int64(v)
			b = b[vn:]
		case fieldNum == 9 && wireType == 5:
			bits, vn, err := readProtoFixed32(b)
			if err != nil {
				return ch, err
			}
			ch.ChapterNumber = cleanFloat32ToFloat64(math.Float32frombits(bits))
			b = b[vn:]
		case fieldNum == 9 && wireType == 1:
			if len(b) < 8 {
				return ch, errors.New("truncated protobuf fixed64 chapterNumber")
			}
			ch.ChapterNumber = math.Float64frombits(binary.LittleEndian.Uint64(b[:8]))
			b = b[8:]
		default:
			vn, err := skipProtoValue(b, wireType)
			if err != nil {
				return ch, err
			}
			b = b[vn:]
		}
	}
	if dateFetch != 0 {
		ch.UpdatedAt = dateFetch
	} else {
		ch.UpdatedAt = dateUpload
	}
	return ch, nil
}

func decodeBackupManga(b []byte) (rawDecodedManga, error) {
	// In Tachiyomi / Mihon / Karasu BackupManga, favorite defaults to true when omitted.
	out := rawDecodedManga{
		manga: SyncManga{Favorite: true},
	}
	for len(b) > 0 {
		fieldNum, wireType, n, err := readProtoTag(b)
		if err != nil {
			return out, err
		}
		b = b[n:]
		switch {
		case fieldNum == 1 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return out, err
			}
			out.manga.SourceID = int64(v)
			b = b[vn:]
		case fieldNum == 2 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return out, err
			}
			out.manga.URL = string(sb)
			b = b[vn:]
		case fieldNum == 3 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return out, err
			}
			out.manga.Title = string(sb)
			b = b[vn:]
		case fieldNum == 9 && wireType == 2:
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return out, err
			}
			out.manga.ThumbnailURL = strings.TrimSpace(string(sb))
			b = b[vn:]
		case fieldNum == 13 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return out, err
			}
			out.manga.UpdatedAt = int64(v)
			b = b[vn:]
		case fieldNum == 16 && wireType == 2:
			cb, vn, err := readProtoBytes(b)
			if err != nil {
				return out, err
			}
			ch, err := decodeBackupChapter(cb)
			if err != nil {
				return out, err
			}
			out.manga.Chapters = append(out.manga.Chapters, ch)
			b = b[vn:]
		case fieldNum == 17 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return out, err
			}
			out.catIDs = append(out.catIDs, int(int64(v)))
			b = b[vn:]
		case fieldNum == 17 && wireType == 2:
			pb, vn, err := readProtoBytes(b)
			if err != nil {
				return out, err
			}
			for len(pb) > 0 {
				v, pn, err := readProtoVarint(pb)
				if err != nil {
					return out, err
				}
				out.catIDs = append(out.catIDs, int(int64(v)))
				pb = pb[pn:]
			}
			b = b[vn:]
		case fieldNum == 100 && wireType == 0:
			v, vn, err := readProtoVarint(b)
			if err != nil {
				return out, err
			}
			out.manga.Favorite = v != 0
			b = b[vn:]
		default:
			vn, err := skipProtoValue(b, wireType)
			if err != nil {
				return out, err
			}
			b = b[vn:]
		}
	}
	return out, nil
}

func decodeBackupExtensionRepo(b []byte) (string, error) {
	var baseURL string
	for len(b) > 0 {
		fieldNum, wireType, n, err := readProtoTag(b)
		if err != nil {
			return "", err
		}
		b = b[n:]
		if fieldNum == 1 && wireType == 2 {
			sb, vn, err := readProtoBytes(b)
			if err != nil {
				return "", err
			}
			baseURL = strings.TrimSpace(string(sb))
			b = b[vn:]
		} else {
			vn, err := skipProtoValue(b, wireType)
			if err != nil {
				return "", err
			}
			b = b[vn:]
		}
	}
	return baseURL, nil
}

// decodeTachibkToSyncPayload gunzips (if gzipped) and decodes a Tachiyomi / Mihon / Karasu
// .tachibk backup archive into a SyncPayload.
func decodeTachibkToSyncPayload(raw []byte) (*SyncPayload, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty .tachibk archive")
	}

	data := raw
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("gunzip .tachibk: %w", err)
		}
		defer gr.Close()
		limited := io.LimitReader(gr, maxDecompressedTachibk+1)
		decomp, err := io.ReadAll(limited)
		if err != nil {
			return nil, fmt.Errorf("read gunzipped .tachibk: %w", err)
		}
		if len(decomp) > maxDecompressedTachibk {
			return nil, errors.New(".tachibk exceeds maximum decompressed size")
		}
		data = decomp
	}

	var (
		rawMangas []rawDecodedManga
		cats      []SyncCategory
		repos     []string
		seenRepos = map[string]bool{}
	)

	b := data
	for len(b) > 0 {
		fieldNum, wireType, n, err := readProtoTag(b)
		if err != nil {
			return nil, fmt.Errorf("corrupt .tachibk protobuf: %w", err)
		}
		b = b[n:]
		switch {
		case fieldNum == 1 && wireType == 2:
			mb, vn, err := readProtoBytes(b)
			if err != nil {
				return nil, err
			}
			rm, err := decodeBackupManga(mb)
			if err != nil {
				return nil, err
			}
			if rm.manga.SourceID != 0 && rm.manga.URL != "" {
				rawMangas = append(rawMangas, rm)
			}
			b = b[vn:]
		case fieldNum == 2 && wireType == 2:
			cb, vn, err := readProtoBytes(b)
			if err != nil {
				return nil, err
			}
			cat, err := decodeBackupCategory(cb)
			if err != nil {
				return nil, err
			}
			if name := strings.TrimSpace(cat.Name); name != "" {
				cat.Name = name
				cats = append(cats, cat)
			}
			b = b[vn:]
		case fieldNum == 106 && wireType == 2:
			rb, vn, err := readProtoBytes(b)
			if err != nil {
				return nil, err
			}
			repoURL, err := decodeBackupExtensionRepo(rb)
			if err != nil {
				return nil, err
			}
			if repoURL != "" && !seenRepos[repoURL] {
				seenRepos[repoURL] = true
				repos = append(repos, repoURL)
			}
			b = b[vn:]
		default:
			vn, err := skipProtoValue(b, wireType)
			if err != nil {
				return nil, err
			}
			b = b[vn:]
		}
	}

	catByOrder := make(map[int]string, len(cats))
	for _, c := range cats {
		catByOrder[c.Order] = c.Name
	}

	out := &SyncPayload{
		Categories:     make([]SyncCategory, 0, len(cats)),
		Mangas:         make([]SyncManga, 0, len(rawMangas)),
		ExtensionRepos: repos,
	}
	for _, c := range cats {
		if !strings.EqualFold(c.Name, "Default") {
			out.Categories = append(out.Categories, c)
		}
	}
	if out.ExtensionRepos == nil {
		out.ExtensionRepos = []string{}
	}

	for _, rm := range rawMangas {
		seenMangaCat := map[string]bool{}
		for _, cid := range rm.catIDs {
			name, ok := catByOrder[cid]
			if !ok && cid >= 0 && cid < len(cats) {
				name = cats[cid].Name
			}
			name = strings.TrimSpace(name)
			if name == "" || strings.EqualFold(name, "Default") || seenMangaCat[name] {
				continue
			}
			seenMangaCat[name] = true
			rm.manga.Categories = append(rm.manga.Categories, name)
		}
		out.Mangas = append(out.Mangas, rm.manga)
	}

	return out, nil
}
