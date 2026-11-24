package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

type visionBubble struct {
	Box  [4]int `json:"box"` // [ymin, xmin, ymax, xmax] normalized 0..1000
	Text string `json:"text"`
}

var (
	bubbleFontOnce sync.Once
	bubbleFont     *opentype.Font
	bubbleFontErr  error
)

func loadBubbleFont() (*opentype.Font, error) {
	bubbleFontOnce.Do(func() {
		bubbleFont, bubbleFontErr = opentype.Parse(gobold.TTF)
	})
	return bubbleFont, bubbleFontErr
}

func humanLangName(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "", "PTB", "PT-BR", "PT_BR", "PT":
		return "Brazilian Portuguese (pt-BR)"
	case "ENG", "EN", "EN-US":
		return "English"
	case "ESP", "ES":
		return "Spanish"
	case "FRA", "FR":
		return "French"
	case "DEU", "DE":
		return "German"
	case "ITA", "IT":
		return "Italian"
	default:
		return code
	}
}

// translatePageWithVisionLLM detects speech bubbles, translates them via the
// configured Vision LLM (Gemini or OpenAI-compatible), erases the original
// text cleanly, and typesets the translated dialogue directly onto the full-res
// manga page — with zero external Python/PyTorch sidecars.
func translatePageWithVisionLLM(ctx context.Context, client *http.Client, cur settings, imgBytes []byte, targetLang string) ([]byte, error) {
	srcImg, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("decode manga page: %w", err)
	}

	apiJPEG, err := encodeVisionJPEG(srcImg, 1600)
	if err != nil {
		return nil, fmt.Errorf("prepare vision jpeg: %w", err)
	}

	bubbles, err := callVisionBubbleTranslator(ctx, client, cur, apiJPEG, targetLang)
	if err != nil {
		return nil, err
	}

	outImg := renderTranslatedBubbles(srcImg, bubbles)
	var buf bytes.Buffer
	if err := png.Encode(&buf, outImg); err != nil {
		return nil, fmt.Errorf("encode translated png: %w", err)
	}
	return buf.Bytes(), nil
}

// encodeVisionJPEG scales oversized pages down to maxDim on their longest edge
// so Vision LLM calls stay fast and compact, while preserving aspect ratio for
// exact 0..1000 normalized bounding box mapping back to full resolution.
func encodeVisionJPEG(src image.Image, maxDim int) ([]byte, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var target image.Image = src
	if w > maxDim || h > maxDim {
		scale := float64(maxDim) / float64(max(w, h))
		nw := max(1, int(math.Round(float64(w)*scale)))
		nh := max(1, int(math.Round(float64(h)*scale)))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
		target = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, target, &jpeg.Options{Quality: 84}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func callVisionBubbleTranslator(ctx context.Context, client *http.Client, cur settings, jpegBytes []byte, targetLang string) ([]visionBubble, error) {
	langName := humanLangName(targetLang)
	prompt := fmt.Sprintf(
		"You are an expert manga translator and typesetter. "+
			"Inspect this manga page, locate every speech bubble, thought bubble, and narration box containing readable dialogue, "+
			"and translate each one into natural, concise %s that fits inside the bubble. "+
			"Skip tiny decorative background SFX unless plot-critical. "+
			"Return ONLY a JSON object with this exact shape: "+
			`{"bubbles":[{"box":[ymin,xmin,ymax,xmax],"text":"translated dialogue"}]}`+
			" where box coordinates are integers normalized from 0 to 1000 (0=top/left, 1000=bottom/right), "+
			"tightly framing the text area inside each bubble. "+
			`If the page has no dialogue, return {"bubbles":[]}.`,
		langName,
	)

	b64 := base64.StdEncoding.EncodeToString(jpegBytes)
	var rawContent string
	var err error

	switch cur.TranslateProvider {
	case "gemini":
		rawContent, err = callGeminiVision(ctx, client, cur, prompt, b64)
	case "openai":
		rawContent, err = callOpenAICompatVision(ctx, client, cur, prompt, b64)
	default:
		return nil, fmt.Errorf("unsupported translate provider %q", cur.TranslateProvider)
	}
	if err != nil {
		return nil, err
	}

	cleaned := stripJSONFences(rawContent)
	var parsed struct {
		Bubbles []visionBubble `json:"bubbles"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("vision llm returned invalid json: %w", err)
	}
	return parsed.Bubbles, nil
}

func callGeminiVision(ctx context.Context, client *http.Client, cur settings, prompt, b64JPEG string) (string, error) {
	model := cur.TranslateModel
	if model == "" {
		model = "gemini-2.5-flash"
	}
	endpoint := cur.TranslateEndpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
					{
						"inline_data": map[string]string{
							"mime_type": "image/jpeg",
							"data":      b64JPEG,
						},
					},
				},
			},
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
		return "", fmt.Errorf("gemini vision http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
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
		return "", errors.New("empty vision response from gemini")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

func callOpenAICompatVision(ctx context.Context, client *http.Client, cur settings, prompt, b64JPEG string) (string, error) {
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
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": "data:image/jpeg;base64," + b64JPEG,
						},
					},
				},
			},
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
		return "", fmt.Errorf("openai vision http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
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
		return "", errors.New("empty choices from openai-compatible vision provider")
	}
	return out.Choices[0].Message.Content, nil
}

// renderTranslatedBubbles composites all translated speech bubbles onto a fresh
// RGBA copy of src, sampling each bubble's native background color, erasing the
// original text with a feathered rounded mask, and auto-fitting centered bold text.
func renderTranslatedBubbles(src image.Image, bubbles []visionBubble) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	ttf, err := loadBubbleFont()
	if err != nil || len(bubbles) == 0 {
		return dst
	}

	for _, bub := range bubbles {
		text := strings.TrimSpace(bub.Text)
		if text == "" {
			continue
		}
		y0 := clampInt(bub.Box[0]*h/1000, 0, h-1)
		x0 := clampInt(bub.Box[1]*w/1000, 0, w-1)
		y1 := clampInt(bub.Box[2]*h/1000, 0, h)
		x1 := clampInt(bub.Box[3]*w/1000, 0, w)
		if x1-x0 < 16 || y1-y0 < 16 {
			continue
		}

		rect := image.Rect(x0, y0, x1, y1)
		bg := sampleBubbleBackground(dst, rect)
		fillFeatheredRoundedRect(dst, rect, bg)
		drawFittedBubbleText(dst, rect, text, ttf, bg)
	}
	return dst
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sampleBubbleBackground inspects the inner perimeter of the detected bubble box
// and returns the median color of the dominant brightness class (bright paper vs
// dark dramatic bubble), avoiding ink strokes from the original text.
func sampleBubbleBackground(img *image.RGBA, r image.Rectangle) color.RGBA {
	var brightR, brightG, brightB []int
	var darkR, darkG, darkB []int
	sample := func(x, y int) {
		c := img.RGBAAt(x, y)
		lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
		if lum >= 140 {
			brightR = append(brightR, int(c.R))
			brightG = append(brightG, int(c.G))
			brightB = append(brightB, int(c.B))
		} else {
			darkR = append(darkR, int(c.R))
			darkG = append(darkG, int(c.G))
			darkB = append(darkB, int(c.B))
		}
	}
	stepX := max(1, r.Dx()/16)
	stepY := max(1, r.Dy()/16)
	for x := r.Min.X; x < r.Max.X; x += stepX {
		sample(x, r.Min.Y)
		sample(x, r.Max.Y-1)
	}
	for y := r.Min.Y; y < r.Max.Y; y += stepY {
		sample(r.Min.X, y)
		sample(r.Max.X-1, y)
	}
	if len(brightR) >= len(darkR) && len(brightR) > 0 {
		return color.RGBA{R: uint8(medianInt(brightR)), G: uint8(medianInt(brightG)), B: uint8(medianInt(brightB)), A: 255}
	}
	if len(darkR) > 0 {
		return color.RGBA{R: uint8(medianInt(darkR)), G: uint8(medianInt(darkG)), B: uint8(medianInt(darkB)), A: 255}
	}
	return color.RGBA{R: 255, G: 255, B: 255, A: 255}
}

func medianInt(vals []int) int {
	if len(vals) == 0 {
		return 255
	}
	sort.Ints(vals)
	return vals[len(vals)/2]
}

// fillFeatheredRoundedRect paints a rounded rectangle with a soft 3px feathered
// edge so erased bubbles blend naturally into manga speech balloons.
func fillFeatheredRoundedRect(img *image.RGBA, r image.Rectangle, bg color.RGBA) {
	w := float64(r.Dx())
	h := float64(r.Dy())
	rad := math.Min(w, h) * 0.22
	const feather = 3.0

	cx := float64(r.Min.X) + w/2
	cy := float64(r.Min.Y) + h/2
	halfW := w/2 - rad
	halfH := h/2 - rad

	for y := r.Min.Y; y < r.Max.Y; y++ {
		dy := math.Abs(float64(y)+0.5-cy) - halfH
		for x := r.Min.X; x < r.Max.X; x++ {
			dx := math.Abs(float64(x)+0.5-cx) - halfW
			// Signed distance to rounded rectangle boundary (negative = inside)
			ox := math.Max(dx, 0)
			oy := math.Max(dy, 0)
			dist := math.Hypot(ox, oy) + math.Min(math.Max(dx, dy), 0) - rad
			if dist >= 0 {
				continue
			}
			alpha := 1.0
			if dist > -feather {
				alpha = -dist / feather
			}
			if alpha >= 0.999 {
				img.SetRGBA(x, y, bg)
			} else if alpha > 0 {
				dst := img.RGBAAt(x, y)
				inv := 1.0 - alpha
				img.SetRGBA(x, y, color.RGBA{
					R: uint8(float64(bg.R)*alpha + float64(dst.R)*inv),
					G: uint8(float64(bg.G)*alpha + float64(dst.G)*inv),
					B: uint8(float64(bg.B)*alpha + float64(dst.B)*inv),
					A: 255,
				})
			}
		}
	}
}

func drawFittedBubbleText(img *image.RGBA, r image.Rectangle, text string, ttf *opentype.Font, bg color.RGBA) {
	padX := max(6, r.Dx()/12)
	padY := max(6, r.Dy()/12)
	innerW := r.Dx() - 2*padX
	innerH := r.Dy() - 2*padY
	if innerW < 10 || innerH < 10 {
		return
	}

	lum := (299*int(bg.R) + 587*int(bg.G) + 114*int(bg.B)) / 1000
	fg := color.RGBA{R: 20, G: 20, B: 20, A: 255}
	halo := color.RGBA{R: 255, G: 255, B: 255, A: 220}
	if lum < 140 {
		fg = color.RGBA{R: 250, G: 250, B: 250, A: 255}
		halo = color.RGBA{R: 15, G: 15, B: 15, A: 220}
	}

	maxFont := min(44, max(12, innerH/2))
	const minFont = 10

	var bestFace font.Face
	var bestLines []string
	var bestLineH int

	for sz := maxFont; sz >= minFont; sz -= 2 {
		face, err := opentype.NewFace(ttf, &opentype.FaceOptions{
			Size:    float64(sz),
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		lines := wrapTextToWidth(face, text, innerW)
		metrics := face.Metrics()
		lineH := int(math.Ceil(float64(metrics.Height.Ceil()) * 1.15))
		totalH := len(lines) * lineH
		if totalH <= innerH || sz == minFont {
			if bestFace != nil {
				bestFace.Close()
			}
			bestFace = face
			bestLines = lines
			bestLineH = lineH
			if totalH <= innerH {
				break
			}
		} else {
			face.Close()
		}
	}
	if bestFace == nil || len(bestLines) == 0 {
		return
	}
	defer bestFace.Close()

	ascent := bestFace.Metrics().Ascent.Ceil()
	totalH := len(bestLines) * bestLineH
	startY := r.Min.Y + padY + max(0, (innerH-totalH)/2) + ascent

	drawPass := func(col color.RGBA, dx, dy int) {
		d := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(col),
			Face: bestFace,
		}
		for i, line := range bestLines {
			adv := font.MeasureString(bestFace, line).Ceil()
			x := r.Min.X + padX + max(0, (innerW-adv)/2) + dx
			y := startY + i*bestLineH + dy
			if y > r.Max.Y-2 {
				break
			}
			d.Dot = fixed.P(x, y)
			d.DrawString(line)
		}
	}

	// Crisp 1px contrasting halo + main text pass.
	for _, off := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		drawPass(halo, off[0], off[1])
	}
	drawPass(fg, 0, 0)
}

func wrapTextToWidth(face font.Face, text string, maxWidth int) []string {
	paragraphs := strings.Split(text, "\n")
	var out []string
	for _, para := range paragraphs {
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		cur := ""
		for _, w := range words {
			candidate := w
			if cur != "" {
				candidate = cur + " " + w
			}
			if font.MeasureString(face, candidate).Ceil() <= maxWidth {
				cur = candidate
				continue
			}
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			if font.MeasureString(face, w).Ceil() <= maxWidth {
				cur = w
				continue
			}
			// Hard-break an oversized single word so it stays inside the bubble.
			runes := []rune(w)
			chunk := ""
			for _, rn := range runes {
				next := chunk + string(rn)
				if font.MeasureString(face, next).Ceil() > maxWidth && chunk != "" {
					out = append(out, chunk)
					chunk = string(rn)
				} else {
					chunk = next
				}
			}
			cur = chunk
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}
