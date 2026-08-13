package main

import (
	"encoding/json"
	"math/rand"
	"strings"
	"syscall/js"
	"time"
)

const (
	logicalW = 1080
	logicalH = 1980
	timeout  = 2 * time.Minute
)

type viewer struct {
	ID, Name, Avatar, Color string
	X, Y                    float64
	LastSeen                time.Time
	PulseUntil              time.Time
	Image                   js.Value
	ImageReady              bool
}

type arena struct {
	ctx       js.Value
	canvas    js.Value
	viewers   map[string]*viewer
	frame     js.Func
	event     js.Func
	imageLoad map[string]js.Func
}

func main() {
	canvas := js.Global().Get("document").Call("getElementById", "arena")
	canvas.Set("width", logicalW)
	canvas.Set("height", logicalH)
	a := &arena{
		canvas:    canvas,
		ctx:       canvas.Call("getContext", "2d"),
		viewers:   map[string]*viewer{},
		imageLoad: map[string]js.Func{},
	}
	a.connectEvents()
	a.frame = js.FuncOf(func(js.Value, []js.Value) interface{} {
		a.update()
		a.draw()
		js.Global().Call("requestAnimationFrame", a.frame)
		return nil
	})
	js.Global().Call("requestAnimationFrame", a.frame)
	select {}
}

func (a *arena) update() {
	now := time.Now()
	for id, v := range a.viewers {
		if now.Sub(v.LastSeen) > timeout {
			delete(a.viewers, id)
		}
	}
}

func (a *arena) draw() {
	ctx := a.ctx
	ctx.Call("clearRect", 0, 0, logicalW, logicalH)

	gradient := ctx.Call("createRadialGradient", 540, 730, 40, 540, 730, 1300)
	gradient.Call("addColorStop", 0, "#304d78")
	gradient.Call("addColorStop", 0.45, "#12152a")
	gradient.Call("addColorStop", 1, "#170d1e")
	ctx.Set("fillStyle", gradient)
	ctx.Call("fillRect", 0, 0, logicalW, logicalH)

	ctx.Set("strokeStyle", "rgba(255,255,255,0.04)")
	ctx.Set("lineWidth", 1)
	for x := 0; x < logicalW; x += 96 {
		ctx.Call("beginPath")
		ctx.Call("moveTo", x, 0)
		ctx.Call("lineTo", x, logicalH)
		ctx.Call("stroke")
	}
	for y := 0; y < logicalH; y += 96 {
		ctx.Call("beginPath")
		ctx.Call("moveTo", 0, y)
		ctx.Call("lineTo", logicalW, y)
		ctx.Call("stroke")
	}

	ctx.Set("fillStyle", "white")
	ctx.Set("font", "900 48px Segoe UI, system-ui, sans-serif")
	ctx.Set("textAlign", "center")
	ctx.Set("shadowColor", "rgba(0,0,0,0.8)")
	ctx.Set("shadowBlur", 20)
	ctx.Call("fillText", "AVATAR ARENA", logicalW/2, 192)
	ctx.Set("shadowBlur", 0)

	for _, v := range a.viewers {
		a.drawViewer(v)
	}
	if len(a.viewers) == 0 {
		ctx.Set("fillStyle", "rgba(255,255,255,0.6)")
		ctx.Set("font", "24px Segoe UI, system-ui, sans-serif")
		ctx.Call("fillText", "Waiting for viewers...", logicalW/2, 990)
	}
}

func (a *arena) drawViewer(v *viewer) {
	ctx := a.ctx
	color := v.Color
	if color == "" {
		color = "#5de1e6"
	}
	pulsing := time.Now().Before(v.PulseUntil)
	radius := 50.0
	if pulsing {
		radius = 62
	}

	ctx.Call("save")
	ctx.Set("shadowColor", "rgba(0,0,0,0.53)")
	ctx.Set("shadowBlur", 14)
	ctx.Set("shadowOffsetY", 5)
	ctx.Call("beginPath")
	ctx.Call("arc", v.X, v.Y, radius, 0, 2*3.141592653589793)
	ctx.Set("fillStyle", color)
	ctx.Call("fill")
	ctx.Set("shadowColor", "transparent")
	ctx.Set("shadowOffsetY", 0)
	ctx.Set("lineWidth", 3)
	ctx.Set("strokeStyle", "rgba(255,255,255,0.6)")
	ctx.Call("stroke")

	if v.ImageReady {
		ctx.Call("save")
		ctx.Call("beginPath")
		ctx.Call("arc", v.X, v.Y, radius-3, 0, 2*3.141592653589793)
		ctx.Call("clip")
		ctx.Call("drawImage", v.Image, v.X-radius+3, v.Y-radius+3, (radius-3)*2, (radius-3)*2)
		ctx.Call("restore")
	} else {
		ctx.Set("fillStyle", "#080a11")
		ctx.Set("font", "900 28px Segoe UI, system-ui, sans-serif")
		ctx.Set("textAlign", "center")
		ctx.Call("fillText", initials(v.Name), v.X, v.Y+10)
	}
	ctx.Set("fillStyle", "white")
	ctx.Set("font", "700 20px Segoe UI, system-ui, sans-serif")
	ctx.Set("textAlign", "center")
	ctx.Set("shadowColor", "rgba(0,0,0,0.8)")
	ctx.Set("shadowBlur", 4)
	ctx.Call("fillText", v.Name, v.X, v.Y+86)
	ctx.Call("restore")
}

func (a *arena) connectEvents() {
	a.event = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 {
			return nil
		}
		var raw struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(args[0].Get("data").String()), &raw) != nil {
			return nil
		}
		if raw.Type == "live_end" {
			a.viewers = map[string]*viewer{}
			return nil
		}
		var e struct {
			Type   string `json:"type"`
			Viewer struct {
				ID, Username, AvatarURL, Color string
			} `json:"viewer"`
		}
		if json.Unmarshal([]byte(args[0].Get("data").String()), &e) != nil || e.Viewer.ID == "" {
			return nil
		}
		v := a.viewers[e.Viewer.ID]
		if v == nil {
			v = &viewer{
				ID: e.Viewer.ID, Name: e.Viewer.Username, Avatar: e.Viewer.AvatarURL,
				Color: e.Viewer.Color, X: 140 + rand.Float64()*800, Y: 420 + rand.Float64()*1120,
				LastSeen: time.Now(),
			}
			a.viewers[v.ID] = v
			a.loadAvatar(v)
		}
		v.LastSeen = time.Now()
		if e.Type != "join" {
			v.PulseUntil = time.Now().Add(500 * time.Millisecond)
		}
		return nil
	})
	es := js.Global().Get("EventSource").New("/api/events")
	es.Call("addEventListener", "message", a.event)
}

func (a *arena) loadAvatar(v *viewer) {
	if v.Avatar == "" {
		return
	}
	img := js.Global().Get("Image").New()
	callback := js.FuncOf(func(js.Value, []js.Value) interface{} {
		v.Image = img
		v.ImageReady = true
		return nil
	})
	a.imageLoad[v.ID] = callback
	img.Set("onload", callback)
	img.Set("src", v.Avatar)
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "??"
	}
	if len(parts) == 1 {
		runes := []rune(parts[0])
		if len(runes) > 2 {
			runes = runes[:2]
		}
		return strings.ToUpper(string(runes))
	}
	return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[1])[0]))
}
