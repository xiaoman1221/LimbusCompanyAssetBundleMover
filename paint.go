package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/lxn/walk"
)

const (
	padX       = 30.0
	hazardH    = 4.0
	headerH    = 120.0
	footerH    = 32.0
	sectionGap = 14.0
	bodyTopGap = 20.0
)

type buttonStyle int

const (
	btnPrimary buttonStyle = iota
	btnGhost
	btnDanger
)

type painter struct {
	cv    *walk.Canvas
	scale float64
	w, h  int
	dw    float64
	dh    float64
}

func (a *app) paint(cv *walk.Canvas, _ walk.Rectangle) error {
	// Canvas.BoundsPixels() reports the display size for buffered paint
	// canvases, so take the widget's real client rectangle instead.
	b := a.cw.BoundsPixels()
	scale := float64(cv.DPI()) / 96.0
	if scale <= 0 {
		scale = 1
	}
	p := &painter{
		cv:    cv,
		scale: scale,
		w:     b.Width,
		h:     b.Height,
	}
	p.dw = float64(p.w) / p.scale
	p.dh = float64(p.h) / p.scale

	a.hits = a.hits[:0]

	a.paintBackdrop(p)
	a.paintHeader(p)
	a.paintBody(p)
	a.paintFooter(p)
	if a.promptOpen {
		a.paintPrompt(p)
	}

	return nil
}

func (p *painter) d(v float64) int {
	return int(math.Round(v * p.scale))
}

func (p *painter) rect(x, y, w, h float64) walk.Rectangle {
	return walk.Rectangle{X: p.d(x), Y: p.d(y), Width: p.d(w), Height: p.d(h)}
}

func (p *painter) fill(c walk.Color, x, y, w, h float64) {
	if w <= 0 || h <= 0 {
		return
	}
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		return
	}
	defer br.Dispose()
	_ = p.cv.FillRectanglePixels(br, p.rect(x, y, w, h))
}

func (p *painter) frame(c walk.Color, x, y, w, h, t float64) {
	p.fill(c, x, y, w, t)
	p.fill(c, x, y+h-t, w, t)
	p.fill(c, x, y+t, t, h-2*t)
	p.fill(c, x+w-t, y+t, t, h-2*t)
}

func (p *painter) line(c walk.Color, x1, y1, x2, y2, width float64) {
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		return
	}
	defer br.Dispose()

	w := int(math.Round(width))
	if w < 1 {
		w = 1
	}
	pen, err := walk.NewGeometricPen(walk.PenSolid, w, br)
	if err != nil {
		return
	}
	defer pen.Dispose()

	_ = p.cv.DrawLinePixels(pen,
		walk.Point{X: p.d(x1), Y: p.d(y1)},
		walk.Point{X: p.d(x2), Y: p.d(y2)},
	)
}

func (p *painter) ellipseOutline(c walk.Color, cx, cy, r, width float64) {
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		return
	}
	defer br.Dispose()
	w := int(math.Round(width))
	if w < 1 {
		w = 1
	}
	pen, err := walk.NewGeometricPen(walk.PenSolid, w, br)
	if err != nil {
		return
	}
	defer pen.Dispose()
	_ = p.cv.DrawEllipsePixels(pen, p.rect(cx-r, cy-r, 2*r, 2*r))
}

func (p *painter) ellipseFill(c walk.Color, cx, cy, r float64) {
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		return
	}
	defer br.Dispose()
	_ = p.cv.FillEllipsePixels(br, p.rect(cx-r, cy-r, 2*r, 2*r))
}

func (p *painter) text(s string, f *walk.Font, c walk.Color, x, y, w, h float64, flags walk.DrawTextFormat) {
	if s == "" || w <= 0 || h <= 0 {
		return
	}
	_ = p.cv.DrawTextPixels(s, f, c, p.rect(x, y, w, h), flags|walk.TextSingleLine|walk.TextNoPrefix)
}

// ---------------------------------------------------------------------------
// backdrop

func (a *app) paintBackdrop(p *painter) {
	p.cv.GradientFillRectanglePixels(colBG, colBGDeep, walk.Vertical, p.rect(0, 0, p.dw, p.dh))

	for x := 56.0; x < p.dw; x += 56 {
		p.fill(colLineDim, x, 0, 1, p.dh)
	}
	for y := 56.0; y < p.dh; y += 56 {
		p.fill(colLineDim, 0, y, p.dw, 1)
	}

	p.fill(colHeaderLo, 0, 0, p.dw, hazardH)
	for x := -6.0; x < p.dw+6; x += 13 {
		p.line(colGoldDeep, x, hazardH, x+hazardH, 0, 3)
	}
}

// ---------------------------------------------------------------------------
// header

func (a *app) paintHeader(p *painter) {
	y0 := hazardH
	p.cv.GradientFillRectanglePixels(colHeaderHi, colHeaderLo, walk.Vertical, p.rect(0, y0, p.dw, headerH))

	pad := padX

	p.text("PROJECT MOON  //  资源迁移终端", a.th.label, colGoldDim,
		pad, 16, 560, 12, walk.TextLeft|walk.TextVCenter)

	p.fill(colRed, pad-12, 37, 3, 42)
	p.text("LIMBUS COMPANY", a.th.display, colGold,
		pad, 30, 640, 48, walk.TextLeft|walk.TextVCenter)
	p.text("资源包迁移工具", a.th.heading, colText,
		pad, 82, 460, 18, walk.TextLeft|walk.TextVCenter)
	p.text("C 盘空间回收    /    目录符号链接    /    完整性校验",
		a.th.small, colTextFaint, pad, 101, 620, 12, walk.TextLeft|walk.TextVCenter)

	p.text("终端版本 0.1.0", a.th.small, colTextFaint,
		p.dw-pad-260, 16, 260, 12, walk.TextRight|walk.TextVCenter)

	cx, cy, r := p.dw-pad-44, 64.0, 32.0
	a.paintEmblem(p, cx, cy, r)
	p.text("节点 // 01", a.th.label, colGoldDim,
		cx-65, 101, 130, 12, walk.TextCenter|walk.TextVCenter)

	dy := y0 + headerH
	p.fill(colLine, 0, dy, p.dw, 1)
	p.fill(colGold, pad, dy-1, 190, 2)
	p.fill(colRed, pad+190, dy-1, 40, 2)
	for x := pad; x < p.dw-pad; x += 32 {
		p.fill(colLineDim, x, dy+3, 1, 3)
	}
}

func (a *app) paintEmblem(p *painter, cx, cy, r float64) {
	p.ellipseOutline(colGoldDim, cx, cy, r, 1)
	p.ellipseOutline(colLine, cx, cy, r-7, 1)

	for i := 0; i < 48; i++ {
		ang := float64(i) * math.Pi / 24
		inner := r - 3
		if i%4 == 0 {
			inner = r - 6
		}
		p.line(colGoldDim,
			cx+math.Cos(ang)*inner, cy+math.Sin(ang)*inner,
			cx+math.Cos(ang)*r, cy+math.Sin(ang)*r, 1)
	}
	for i := 0; i < 4; i++ {
		ang := float64(i)*math.Pi/2 - math.Pi/2
		p.line(colRedHi,
			cx+math.Cos(ang)*(r-9), cy+math.Sin(ang)*(r-9),
			cx+math.Cos(ang)*(r-2), cy+math.Sin(ang)*(r-2), 2)
	}

	nx := cx + math.Cos(-math.Pi/4)*r*0.62
	ny := cy + math.Sin(-math.Pi/4)*r*0.62
	p.line(colOrange, cx, cy, nx, ny, 3)

	tx := cx + math.Cos(math.Pi*0.75)*r*0.34
	ty := cy + math.Sin(math.Pi*0.75)*r*0.34
	p.line(colRed, cx, cy, tx, ty, 3)

	p.ellipseFill(colGold, cx, cy, 2.6)
}

// ---------------------------------------------------------------------------
// body

func (a *app) paintBody(p *painter) {
	x := padX
	w := p.dw - 2*padX
	y := hazardH + headerH + bodyTopGap

	y = a.paintSourceSection(p, x, y, w)
	y = a.paintTargetSection(p, x, y+sectionGap, w)
	y = a.paintOperations(p, x, y+sectionGap, w)
	a.paintLog(p, x, y+sectionGap, w, p.dh-footerH-14)
}

func (a *app) sectionHeader(p *painter, x, y, w float64, idx, title, right string, rightColor walk.Color) float64 {
	p.fill(colRed, x, y, 3, 13)
	p.text(idx, a.th.label, colGoldDim, x+10, y-1, 40, 15, walk.TextLeft|walk.TextVCenter)
	p.text(title, a.th.heading, colText, x+36, y-2, w-160, 16, walk.TextLeft|walk.TextVCenter)
	if right != "" {
		p.text(right, a.th.label, rightColor, x, y, w, 14, walk.TextRight|walk.TextVCenter)
	}
	p.fill(colLine, x, y+19, w, 1)
	return y + 22
}

func (a *app) paintPathField(p *painter, x, y, w, h float64, path string, textColor walk.Color, accent walk.Color, reserveRight float64) {
	p.fill(colPanel, x, y, w, h)
	p.frame(colLine, x, y, w, h, 1)
	p.fill(accent, x, y, 3, h)

	text := path
	if text == "" {
		text = "— 未选择路径 —"
	}
	p.text(text, a.th.mono, textColor, x+16, y, w-16-reserveRight, h,
		walk.TextLeft|walk.TextVCenter|walk.TextPathEllipsis)
}

func (a *app) paintSourceSection(p *painter, x, y, w float64) float64 {
	status, statusColor := a.sourceStatusText()
	y = a.sectionHeader(p, x, y, w, "01", "源资源目录", status, statusColor)

	const (
		boxH = 44.0
		btnW = 80.0
		btnH = 26.0
	)
	by := y + 8

	textColor := colMono
	if a.srcState == srcMissing {
		textColor = colErr
	}
	a.paintPathField(p, x, by, w, boxH, a.source, textColor, colRed, 184)

	byBtn := by + (boxH-btnH)/2
	scanX := x + w - 9 - btnW
	locX := scanX - 6 - btnW
	a.paintButton(p, "source.locate", "定位", locX, byBtn, btnW, btnH, btnGhost, true)
	a.paintButton(p, "source.scan", "重新扫描", scanX, byBtn, btnW, btnH, btnGhost, true)

	my := by + boxH + 7
	mx := x
	mx = a.metaItem(p, mx, my, "大小", a.sourceSizeText(), colGoldSoft)
	_ = a.metaItem(p, mx, my, "文件数", a.sourceFilesText(), colText)
	if a.source != "" {
		p.text("源盘符  "+volumeOf(a.source), a.th.small, colTextFaint,
			x, my-1, w, 12, walk.TextRight|walk.TextVCenter)
	}
	return my + 16
}

func (a *app) paintTargetSection(p *painter, x, y, w float64) float64 {
	status, statusColor := "未设置", colTextDim
	if a.target != "" {
		status, statusColor = "就绪", colGoldSoft
	}
	y = a.sectionHeader(p, x, y, w, "02", "目标存放目录", status, statusColor)

	const (
		boxH = 44.0
		btnW = 86.0
		btnH = 26.0
	)
	by := y + 8

	textColor := colMono
	if a.target == "" {
		textColor = colTextFaint
	}
	a.paintPathField(p, x, by, w, boxH, a.target, textColor, colGoldDim, 190)

	byBtn := by + (boxH-btnH)/2
	a.paintButton(p, "target.browse", "浏览", x+w-9-btnW, byBtn, btnW, btnH, btnGhost, true)

	my := by + boxH + 7
	mx := x
	mx = a.metaItem(p, mx, my, "剩余", a.targetFreeText(), colGoldSoft)
	a.metaItem(p, mx, my, "容量", a.targetCapacityText(), colText)
	if a.target != "" {
		p.text("目标盘符  "+volumeOf(a.target), a.th.small, colTextFaint,
			x, my-1, w, 12, walk.TextRight|walk.TextVCenter)
	}
	return my + 16
}

func (a *app) metaItem(p *painter, x, y float64, label, value string, valueColor walk.Color) float64 {
	const (
		labelW = 64.0
		slotW  = 210.0
	)
	p.text(label, a.th.label, colTextFaint, x, y, labelW, 12, walk.TextLeft|walk.TextVCenter)
	p.text(value, a.th.small, valueColor, x+labelW, y, slotW-labelW, 12, walk.TextLeft|walk.TextVCenter)
	return x + slotW
}

func (a *app) paintOperations(p *painter, x, y, w float64) float64 {
	status := "按需提权"
	statusColor := colGoldDim
	if a.busy {
		status = "执行中"
		statusColor = colOrange
	}
	y = a.sectionHeader(p, x, y, w, "03", "操作", status, statusColor)

	const h = 44.0
	by := y + 10
	relocateLabel := "迁移到目标目录"
	if a.relocated {
		relocateLabel = "重新链接到新目录"
	}
	a.paintButton(p, "op.relocate", relocateLabel, x, by, 238, h, btnPrimary, false)
	a.paintButton(p, "op.verify", "校验", x+250, by, 128, h, btnGhost, false)
	a.paintButton(p, "op.restore", "还原", x+390, by, 128, h, btnDanger, false)

	ny := by + h + 12
	if a.busy {
		msg := a.opMessage
		if msg == "" {
			msg = a.opLabel + "..."
		}
		p.text(msg, a.th.small, colGoldSoft, x, ny, w, 12, walk.TextLeft|walk.TextVCenter)

		barY := ny + 15
		p.fill(colLine, x, barY, w, 4)
		pct := a.opPercent
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		if pct > 0 {
			p.fill(colGold, x, barY, w*pct/100, 4)
		}
		return ny + 24
	}

	tx, ty := x+1, ny+1
	p.line(colOrange, tx, ty+9, tx+5, ty, 1)
	p.line(colOrange, tx+5, ty, tx+10, ty+9, 1)
	p.line(colOrange, tx, ty+9, tx+10, ty+9, 1)
	warn := "开始迁移前，请先关闭 Limbus Company"
	if a.relocated {
		warn = "重新链接会把现有数据移动到新目录并更新链接，请先关闭 Limbus Company"
	}
	p.text(warn, a.th.small, colOrange, x+18, ny, w-18, 12, walk.TextLeft|walk.TextVCenter)

	return ny + 14
}

func (a *app) paintLog(p *painter, x, y, w, bottom float64) {
	y = a.sectionHeader(p, x, y, w, "04", "系统日志",
		fmt.Sprintf("%d 条", len(a.log)), colTextFaint)

	py := y + 8
	ph := bottom - py
	if ph < 60 {
		ph = 60
	}

	p.fill(colBGDeep, x, py, w, ph)
	p.frame(colLine, x, py, w, ph, 1)
	p.fill(colRed, x, py, 2, ph)
	a.cornerBrackets(p, x, py, w, ph)

	const lineH = 16.0
	maxLines := int((ph - 14) / lineH)
	if maxLines < 1 {
		maxLines = 1
	}
	entries := a.log
	if len(entries) > maxLines {
		entries = entries[len(entries)-maxLines:]
	}

	ly := py + 7
	for _, e := range entries {
		p.text("["+e.ts+"]", a.th.monoSm, colGoldDim,
			x+12, ly, 64, lineH, walk.TextLeft|walk.TextVCenter)
		p.text(e.msg, a.th.log, logColor(e.kind),
			x+82, ly, w-96, lineH, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis)
		ly += lineH
	}
}

func (a *app) paintPrompt(p *painter) {
	p.dimScreen(colBGDeep, 2)

	const cardW, cardH = 660.0, 236.0
	x := (p.dw - cardW) / 2
	y := (p.dh - cardH) / 2

	p.fill(colPanel, x, y, cardW, cardH)
	p.frame(colGoldDim, x, y, cardW, cardH, 1)
	a.cornerBrackets(p, x, y, cardW, cardH)
	p.fill(colGold, x, y, cardW, 2)
	p.fill(colRed, x, y, 96, 2)

	p.fill(colRed, x+24, y+22, 3, 14)
	p.text("检测到已迁移的数据", a.th.heading, colGold,
		x+36, y+18, cardW-60, 20, walk.TextLeft|walk.TextVCenter)
	p.text("源目录已经移动到目标目录，并建立了目录链接。", a.th.small, colText,
		x+24, y+50, cardW-48, 14, walk.TextLeft|walk.TextVCenter)

	p.text("当前目标", a.th.label, colTextFaint,
		x+24, y+76, 70, 14, walk.TextLeft|walk.TextVCenter)
	target := a.target
	if target == "" {
		if t, ok := resolveLink(a.source); ok {
			target = t
		}
	}
	p.text(target, a.th.mono, colMono,
		x+96, y+76, cardW-120, 14, walk.TextLeft|walk.TextVCenter|walk.TextPathEllipsis)

	p.text("请选择下一步操作：", a.th.small, colTextDim,
		x+24, y+104, cardW-48, 14, walk.TextLeft|walk.TextVCenter)

	a.paintButton(p, "prompt.relink", "移动到新目录", x+24, y+132, 190, 42, btnPrimary, false)
	a.paintButton(p, "prompt.restore", "还原到原位置", x+226, y+132, 160, 42, btnDanger, false)
	a.paintButton(p, "prompt.later", "暂不处理", x+398, y+132, 120, 42, btnGhost, false)

	p.text("移动到新目录会复制数据并重建链接；还原会将数据移回源目录。",
		a.th.small, colTextFaint, x+24, y+190, cardW-48, 14, walk.TextLeft|walk.TextVCenter)
}

func (p *painter) dimScreen(c walk.Color, step float64) {
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		return
	}
	defer br.Dispose()
	for y := 0.0; y < p.dh; y += step {
		_ = p.cv.FillRectanglePixels(br, p.rect(0, y, p.dw, 1))
	}
}

func (a *app) cornerBrackets(p *painter, x, y, w, h float64) {
	const s = 10.0
	p.fill(colGoldDim, x, y, s, 1)
	p.fill(colGoldDim, x, y, 1, s)
	p.fill(colGoldDim, x+w-s, y, s, 1)
	p.fill(colGoldDim, x+w-1, y, 1, s)
	p.fill(colGoldDim, x, y+h-1, s, 1)
	p.fill(colGoldDim, x, y+h-s, 1, s)
	p.fill(colGoldDim, x+w-s, y+h-1, s, 1)
	p.fill(colGoldDim, x+w-1, y+h-s, 1, s)
}

// ---------------------------------------------------------------------------
// buttons

func (a *app) paintButton(p *painter, id, label string, x, y, w, h float64, style buttonStyle, compact bool) {
	font := a.th.heading
	if compact {
		font = a.th.label
	}

	if a.busy {
		var bg, border, fg walk.Color
		switch style {
		case btnPrimary:
			bg, border, fg = walk.RGB(0x39, 0x30, 0x22), colGoldDeep, walk.RGB(0x7C, 0x6E, 0x55)
		case btnGhost:
			bg, border, fg = colBG, colLine, colTextFaint
		case btnDanger:
			bg, border, fg = colBG, walk.RGB(0x4A, 0x1A, 0x16), walk.RGB(0x7A, 0x45, 0x3E)
		}
		p.fill(bg, x, y, w, h)
		p.frame(border, x, y, w, h, 1)
		p.text(label, font, fg, x, y, w, h, walk.TextCenter|walk.TextVCenter)
		return
	}

	a.hits = append(a.hits, hitArea{id: id, r: p.rect(x, y, w, h)})

	hover := a.hover == id
	press := a.pressed == id && hover

	var bg, border, fg walk.Color
	switch style {
	case btnPrimary:
		bg, border, fg = colGoldSoft, colGold, walk.RGB(0x14, 0x10, 0x09)
		if hover {
			bg, border = colGold, walk.RGB(0xF7, 0xDC, 0xA8)
		}
		if press {
			bg = walk.RGB(0xA9, 0x7C, 0x3F)
		}
	case btnGhost:
		bg, border, fg = colBG, colGoldDim, colGoldDim
		if hover {
			bg, border, fg = colPanelHi, colGoldSoft, colGoldSoft
		}
		if press {
			bg = walk.RGB(0x08, 0x07, 0x06)
		}
	case btnDanger:
		bg, border, fg = colBG, colRed, walk.RGB(0xA8, 0x50, 0x42)
		if hover {
			bg, border, fg = walk.RGB(0x1E, 0x0C, 0x0A), colRedHi, walk.RGB(0xD0, 0x6A, 0x58)
		}
		if press {
			bg = walk.RGB(0x08, 0x07, 0x06)
		}
	}

	p.fill(bg, x, y, w, h)
	p.frame(border, x, y, w, h, 1)
	if style == btnPrimary {
		p.fill(walk.RGB(0x6B, 0x50, 0x24), x+8, y+h/2-5, 2, 10)
	}
	p.text(label, font, fg, x, y, w, h, walk.TextCenter|walk.TextVCenter)
}

// ---------------------------------------------------------------------------
// footer

func (a *app) paintFooter(p *painter) {
	fy := p.dh - footerH
	p.fill(colBGDeep, 0, fy, p.dw, footerH)
	p.fill(colLine, 0, fy, p.dw, 1)

	status, color := a.terminalStatus()
	p.fill(color, padX, fy+13, 6, 6)
	p.text("状态", a.th.label, colTextFaint, padX+14, fy, 56, footerH, walk.TextLeft|walk.TextVCenter)
	p.text(status, a.th.label, color, padX+64, fy, 220, footerH, walk.TextLeft|walk.TextVCenter)

	p.text(a.diskSummary(), a.th.small, colTextFaint,
		0, fy, p.dw-padX, footerH, walk.TextRight|walk.TextVCenter)
}

// ---------------------------------------------------------------------------
// state text

func (a *app) sourceStatusText() (string, walk.Color) {
	switch {
	case a.scanning:
		return "扫描中", colOrange
	case a.relocated:
		return "已链接", colGold
	case a.srcState == srcPresent:
		return "已检测", colGold
	case a.srcState == srcMissing:
		return "未找到", colRedHi
	default:
		return "未知", colTextDim
	}
}

func (a *app) sourceSizeText() string {
	switch {
	case a.scanning:
		return "扫描中..."
	case a.srcState != srcPresent || a.srcSize <= 0:
		return "—"
	default:
		return humanSize(a.srcSize)
	}
}

func (a *app) sourceFilesText() string {
	if a.scanning || a.srcState != srcPresent {
		return "—"
	}
	return fmt.Sprintf("%d", a.srcFiles)
}

func (a *app) targetFreeText() string {
	if a.target == "" || !a.tgtDiskOK {
		return "—"
	}
	return humanSize(int64(a.tgtFree))
}

func (a *app) targetCapacityText() string {
	if a.target == "" || !a.tgtDiskOK {
		return "—"
	}
	return humanSize(int64(a.tgtTotal))
}

func (a *app) diskSummary() string {
	var parts []string
	if a.source != "" && a.srcDiskOK {
		parts = append(parts, fmt.Sprintf("%s %s 可用", volumeOf(a.source), humanSize(int64(a.srcFree))))
	}
	if a.target != "" && a.tgtDiskOK {
		parts = append(parts, fmt.Sprintf("%s %s 可用", volumeOf(a.target), humanSize(int64(a.tgtFree))))
	}
	if len(parts) == 0 {
		return "无磁盘信息"
	}
	return strings.Join(parts, "   //   ")
}

func logColor(k logKind) walk.Color {
	switch k {
	case logOK:
		return colGoldSoft
	case logWarn:
		return colOrange
	case logErr:
		return colErr
	default:
		return colText
	}
}
