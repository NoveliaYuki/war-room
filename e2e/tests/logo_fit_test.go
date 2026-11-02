package e2e_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// logoEdgeInset sits inside the padding that keeps marks off the avatar border,
// and inside the white frame a solid logo must never show.
const logoEdgeInset = 3

// logoFitFixtureScript draws every logo family in the browser, mounts each in
// the real avatar markup at each avatar shape, and reports where they landed.
const logoFitFixtureScript = `async () => {
	const { renderCompanyAvatar } = await import("/js/avatar.js");
	const BLUE = "#2563eb", INK = "#111111", WHITE = "#ffffff";
	const paint = (color) => (c, w, h) => { c.fillStyle = color; c.fillRect(0, 0, w, h); };
	const rounded = (color, x, y, w, h, r) => (c) => { c.fillStyle = color; c.beginPath(); c.roundRect(x, y, w, h, r); c.fill(); };
	const circle = (color, x, y, r) => (c) => { c.fillStyle = color; c.beginPath(); c.arc(x, y, r, 0, Math.PI * 2); c.fill(); };
	const glyph = (color) => (c, w, h) => { c.fillStyle = color; c.beginPath(); c.moveTo(w * 0.5, h * 0.25); c.lineTo(w * 0.72, h * 0.72); c.lineTo(w * 0.28, h * 0.72); c.fill(); };
	const letters = (color) => (c, w, h) => { c.fillStyle = color; for (let i = 0; i < 6; i += 1) c.fillRect((w / 6) * i + 1, 0, w / 9, h); };
	const gradient = (c, w, h) => { const g = c.createLinearGradient(0, 0, w, h); g.addColorStop(0, "#0ea5e9"); g.addColorStop(1, "#9333ea"); c.fillStyle = g; c.fillRect(0, 0, w, h); };
	const noise = (c, w, h) => { for (let x = 0; x < w; x += 8) for (let y = 0; y < h; y += 8) { c.fillStyle = "hsl(" + ((x * 7 + y * 13) % 360) + ", 60%, 40%)"; c.fillRect(x, y, 8, 8); } };
	const fixtures = [
		["solid square", 128, 128, [paint(BLUE), glyph(WHITE)], "fill"],
		["gradient square", 128, 128, [gradient, glyph(WHITE)], "fill"],
		["patterned photo", 128, 128, [noise], "fill"],
		["black square", 128, 128, [paint(INK), glyph(WHITE)], "fill"],
		["rounded tile", 128, 128, [rounded(BLUE, 0, 0, 128, 128, 28), glyph(WHITE)], "fill"],
		["large radius tile", 128, 128, [rounded(BLUE, 0, 0, 128, 128, 44), glyph(WHITE)], "fill"],
		["tile in transparent margin", 128, 128, [rounded(BLUE, 22, 22, 84, 84, 18), glyph(WHITE)], "fill"],
		["tile in white margin", 128, 128, [paint(WHITE), rounded(BLUE, 22, 22, 84, 84, 18), glyph(WHITE)], "fill"],
		["tile with white corners", 128, 128, [paint(WHITE), rounded(BLUE, 0, 0, 128, 128, 26), glyph(WHITE)], "fill"],
		["off-center tile", 128, 128, [rounded(BLUE, 4, 34, 90, 90, 16)], "fill"],
		["tile with soft shadow", 128, 128, [(c) => { c.shadowColor = "rgba(0,0,0,0.3)"; c.shadowBlur = 14; }, rounded(BLUE, 24, 24, 80, 80, 16)], "fill"],
		["tiny favicon tile", 16, 16, [paint(BLUE), glyph(WHITE)], "fill"],
		["wide dark banner", 600, 300, [paint(INK), glyph(WHITE)], "banner"],
		["tall color banner", 200, 500, [paint(BLUE), glyph(WHITE)], "banner"],
		["wide banner in margin", 400, 400, [rounded("#7c3aed", 0, 120, 400, 160, 10)], "banner"],
		["dark mark on transparent", 128, 128, [glyph(INK)], "mark"],
		["tiny mark on transparent", 16, 16, [glyph(INK)], "mark"],
		["multicolor mark", 128, 128, [circle("#ea4335", 50, 60, 22), circle("#34a853", 78, 60, 22)], "mark"],
		["white mark on transparent", 128, 128, [glyph(WHITE)], "dark"],
		["white wordmark on transparent", 600, 120, [letters(WHITE)], "dark"],
		["circle filling its image", 128, 128, [circle(BLUE, 64, 64, 64)], "mark"],
		["circle in margin", 128, 128, [circle(BLUE, 64, 64, 40)], "mark"],
		["white circle", 128, 128, [circle(WHITE, 64, 64, 44)], "dark"],
		["wordmark touching edges", 600, 120, [letters(BLUE)], "mark"],
		["extreme wordmark", 2560, 100, [letters(INK)], "mark"],
		["tall mark", 100, 400, [letters(INK)], "mark"],
		["wordmark in square canvas", 128, 128, [(c) => { c.fillStyle = INK; for (let i = 0; i < 6; i += 1) c.fillRect(10 + i * 19, 56, 12, 16); }], "mark"],
		["mark clipped by image edge", 128, 128, [circle(BLUE, 0, 64, 50), circle("#f97316", 128, 64, 50)], "mark"],
		["bold mark on white", 128, 128, [paint(WHITE), rounded(INK, 6, 22, 116, 84, 30), (c) => { c.fillStyle = WHITE; c.fillRect(0, 50, 100, 8); c.fillRect(28, 76, 100, 8); c.fillRect(40, 38, 60, 12); c.fillRect(30, 60, 70, 14); }], "mark"],
		["svg mark in margin", 300, 300, [], "mark", "", "<svg xmlns='http://www.w3.org/2000/svg' width='300' height='300'><rect x='20' y='20' width='60' height='24' fill='#2563eb' rx='12'/><circle cx='50' cy='70' r='12' fill='#2563eb'/></svg>"],
		["unsized svg mark", 300, 150, [], "mark", "", "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><path d='M10 10h30v12H22v8h14v12H22v18H10z' fill='#111'/></svg>"],
		["transparent outline ring", 128, 128, [(c) => { c.strokeStyle = INK; c.lineWidth = 6; c.strokeRect(14, 14, 100, 100); }, glyph(INK)], "mark"],
		["tile with thick white border", 128, 128, [paint(BLUE), rounded(WHITE, 4, 4, 120, 120, 12), glyph(BLUE)], "mark"],
		["dark mark on white", 128, 128, [paint(WHITE), glyph(INK)], "mark"],
		["near-white background", 128, 128, [paint("#f8f8f8"), glyph(INK)], "mark"],
		["wordmark on white", 600, 200, [paint(WHITE), letters(BLUE)], "mark"],
		["thin outline on white", 128, 128, [paint(WHITE), (c) => { c.strokeStyle = "#bbbbbb"; c.strokeRect(1, 1, 126, 126); }, glyph(INK)], "mark"],
		["blank white", 128, 128, [paint(WHITE)], "mark"],
		["blank transparent", 128, 128, [], "mark"],
		["noisy jpeg on white", 128, 128, [paint(WHITE), glyph(INK)], "mark", "image/jpeg"],
	];
	const draw = ([, w, h, layers, , type, svg]) => {
		if (svg) return "data:image/svg+xml," + encodeURIComponent(svg);
		const canvas = document.createElement("canvas");
		canvas.width = w;
		canvas.height = h;
		const context = canvas.getContext("2d");
		layers.forEach((layer) => layer(context, w, h));
		return canvas.toDataURL(type || "image/png");
	};
	document.body.innerHTML = "";
	const stage = document.createElement("div");
	stage.style.cssText = "position:fixed;inset:0;display:flex;flex-wrap:wrap;gap:8px;padding:8px;align-content:flex-start;background:#808080;z-index:9999";
	document.body.append(stage);
	const shapes = ["card-avatar", "modal-avatar", "meeting-row-avatar", "card-avatar card-avatar-wide-logo"];
	const tiles = fixtures.flatMap((fixture) => shapes.map((shape) => {
		const tile = document.createElement("div");
		tile.className = shape;
		tile.innerHTML = renderCompanyAvatar("Fixture");
		tile.querySelector("img").removeAttribute("loading");
		tile.querySelector("img").src = draw(fixture);
		stage.append(tile);
		return { tile, name: fixture[0], expected: fixture[4] };
	}));
	// A trimmed mark has no margin left, so its opposite edges must both hold artwork.
	const isContentCentered = (image) => {
		if (!image.src.startsWith("data:image/png") || !image.className.includes("company-logo-mark")) return true;
		const canvas = document.createElement("canvas");
		canvas.width = image.naturalWidth;
		canvas.height = image.naturalHeight;
		const context = canvas.getContext("2d");
		context.drawImage(image, 0, 0);
		const { data } = context.getImageData(0, 0, canvas.width, canvas.height);
		let visible = 0, left = canvas.width, right = -1;
		for (let i = 3, n = 0; i < data.length; i += 4, n += 1) {
			if (data[i] < 24) continue;
			const x = n % canvas.width;
			visible += 1;
			left = Math.min(left, x);
			right = Math.max(right, x);
		}
		return !visible || (canvas.width - 1 - right) + left <= canvas.width * 0.5;
	};
	await Promise.all(tiles.map(({ tile }) => new Promise((resolve) => {
		const image = tile.querySelector("img");
		const settle = () => (image.dataset.logoFitted ? image.decode().then(resolve, resolve) : setTimeout(settle, 20));
		settle();
	})));
	await new Promise((resolve) => setTimeout(resolve, 200));
	return JSON.stringify(tiles.map(({ tile, name, expected }) => {
		const image = tile.querySelector("img");
		const box = tile.getBoundingClientRect();
		const imageBox = image.getBoundingClientRect();
		return {
			name, expected, shape: tile.className, tileClass: image.className, centered: isContentCentered(image),
			backing: getComputedStyle(image).backgroundColor,
			x: box.x, y: box.y, width: box.width, height: box.height,
			imageInside: imageBox.left >= box.left - 0.5 && imageBox.right <= box.right + 0.5 && imageBox.top >= box.top - 0.5 && imageBox.bottom <= box.bottom + 0.5,
		};
	}));
}`

type logoFitResult struct {
	Name        string  `json:"name"`
	Expected    string  `json:"expected"`
	Shape       string  `json:"shape"`
	TileClass   string  `json:"tileClass"`
	Backing     string  `json:"backing"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	ImageInside bool    `json:"imageInside"`
	Centered    bool    `json:"centered"`
}

// TestCompanyLogosFitTheirAvatar guards both failure modes against rendered
// pixels: a white frame around solid logos, and marks reaching the avatar edge.
func TestCompanyLogosFitTheirAvatar(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 1280, 1000)
	encoded, err := page.Evaluate(logoFitFixtureScript, nil)
	if err != nil {
		t.Fatalf("render logo fixtures: %v", err)
	}
	var results []logoFitResult
	if err := json.Unmarshal([]byte(encoded.(string)), &results); err != nil {
		t.Fatalf("decode logo fixtures: %v", err)
	}
	screenshot := captureViewport(t, page)
	for _, result := range results {
		assertLogoFit(t, screenshot, result)
	}
}

func captureViewport(t *testing.T, page playwright.Page) image.Image {
	t.Helper()
	encoded, err := page.Screenshot()
	if err != nil {
		t.Fatalf("capture logo fixtures: %v", err)
	}
	screenshot, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode logo screenshot: %v", err)
	}
	return screenshot
}

func assertLogoFit(t *testing.T, screenshot image.Image, result logoFitResult) {
	t.Helper()
	if !result.ImageInside {
		t.Errorf("%s in %s: image box leaves the avatar", result.Name, result.Shape)
	}
	if !result.Centered {
		t.Errorf("%s in %s: trimmed artwork sits off-center", result.Name, result.Shape)
	}
	if !hasClass(result.TileClass, expectedLogoClass(result.Expected)) {
		t.Errorf("%s in %s: classes %q, want %s", result.Name, result.Shape, result.TileClass, result.Expected)
	}
	edge := avatarEdgePixels(screenshot, result)
	switch result.Expected {
	case "fill":
		if countPixels(edge, isNearWhitePixel) > len(edge)/10 {
			t.Errorf("%s in %s: shows a white frame", result.Name, result.Shape)
		}
	case "mark", "dark":
		if stray := countPixels(edge, func(c color.Color) bool { return !matchesBacking(c, result.Expected == "dark") }); stray > 0 {
			t.Errorf("%s in %s: %d of %d edge pixels overflow the padding", result.Name, result.Shape, stray, len(edge))
		}
	}
}

func expectedLogoClass(expected string) string {
	if expected == "fill" {
		return "company-logo-fills"
	}
	if expected == "banner" {
		return "company-logo-tile"
	}
	return "company-logo-mark"
}

func hasClass(classes, want string) bool {
	return bytes.Contains([]byte(" "+classes+" "), []byte(" "+want+" "))
}

// avatarEdgePixels reads the middle of each side, clear of the rounded corners.
func avatarEdgePixels(screenshot image.Image, result logoFitResult) []color.Color {
	left, top := int(result.X)+logoEdgeInset, int(result.Y)+logoEdgeInset
	right, bottom := int(result.X+result.Width)-logoEdgeInset-1, int(result.Y+result.Height)-logoEdgeInset-1
	quarterWidth, quarterHeight := (right-left)/4, (bottom-top)/4
	var pixels []color.Color
	for x := left + quarterWidth; x <= right-quarterWidth; x++ {
		pixels = append(pixels, screenshot.At(x, top), screenshot.At(x, bottom))
	}
	for y := top + quarterHeight; y <= bottom-quarterHeight; y++ {
		pixels = append(pixels, screenshot.At(left, y), screenshot.At(right, y))
	}
	return pixels
}

func countPixels(pixels []color.Color, matches func(color.Color) bool) int {
	count := 0
	for _, pixel := range pixels {
		if matches(pixel) {
			count++
		}
	}
	return count
}

func channels(c color.Color) (int, int, int) {
	red, green, blue, _ := c.RGBA()
	return int(red >> 8), int(green >> 8), int(blue >> 8)
}

func isNearWhitePixel(c color.Color) bool {
	red, green, blue := channels(c)
	return red > 235 && green > 235 && blue > 235
}

func matchesBacking(c color.Color, dark bool) bool {
	red, green, blue := channels(c)
	if dark {
		return red < 40 && green < 50 && blue < 60
	}
	return red > 245 && green > 245 && blue > 245
}
