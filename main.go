package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"syscall/js"
	"time"
)

const (
	logicalW          = 1080
	logicalH          = 1980
	timeout           = 2 * time.Minute
	maxActiveViewers  = 24
	viewerSlotSpacing = 150.0
	joinedRetention   = 5 * time.Minute
	watchdogInterval  = 250 * time.Millisecond
)

type viewer struct {
	ID, Name, Avatar, Color string
	X, Y                    float64
	VX, VY                  float64
	InteractionSeq          uint64
	InteractionAt           time.Time
	JoinedAt                time.Time
	Scale                   float64
	InteractionScore        int
	RespawnAt               time.Time
	LastSeen                time.Time
	PulseUntil              time.Time
	Image                   js.Value
	ImageReady              bool
	ImageFailed             bool
}

type explosion struct {
	X, Y    float64
	Color   string
	Started time.Time
}

type joinBurst struct {
	X, Y    float64
	Color   string
	Started time.Time
}

type arena struct {
	ctx             js.Value
	canvas          js.Value
	viewers         map[string]*viewer
	joined          map[string]*viewer
	frame           js.Func
	event           js.Func
	imageLoad       map[string]js.Func
	lastFrame       time.Time
	lastWatchdog    time.Time
	sequence        uint64
	watchdogChecks  uint64
	explosions      []explosion
	joinBursts      []joinBurst
	lastInteraction map[string]time.Time
}

func main() {
	canvas := js.Global().Get("document").Call("getElementById", "arena")
	canvas.Set("width", logicalW)
	canvas.Set("height", logicalH)
	a := &arena{
		canvas:          canvas,
		ctx:             canvas.Call("getContext", "2d"),
		viewers:         map[string]*viewer{},
		joined:          map[string]*viewer{},
		imageLoad:       map[string]js.Func{},
		lastInteraction: map[string]time.Time{},
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
	a.runWatchdog(now)
	if a.lastFrame.IsZero() {
		a.lastFrame = now
		return
	}
	dt := now.Sub(a.lastFrame).Seconds()
	a.lastFrame = now
	if dt > 0.05 {
		dt = 0.05
	}
	for _, v := range a.viewers {
		v.X += v.VX * dt
		v.Y += v.VY * dt
		if v.X < 55 {
			v.X, v.VX = 55, math.Abs(v.VX)
		} else if v.X > logicalW-55 {
			v.X, v.VX = logicalW-55, -math.Abs(v.VX)
		}
		if v.Y < 330 {
			v.Y, v.VY = 330, math.Abs(v.VY)
		} else if v.Y > logicalH-100 {
			v.Y, v.VY = logicalH-100, -math.Abs(v.VY)
		}
	}
	a.resolveCollisions()
	activeExplosions := a.explosions[:0]
	for _, effect := range a.explosions {
		if now.Sub(effect.Started) < 550*time.Millisecond {
			activeExplosions = append(activeExplosions, effect)
		}
	}
	a.explosions = activeExplosions
	activeBursts := a.joinBursts[:0]
	for _, burst := range a.joinBursts {
		if now.Sub(burst.Started) < 1100*time.Millisecond {
			activeBursts = append(activeBursts, burst)
		}
	}
	a.joinBursts = activeBursts
}

func (a *arena) runWatchdog(now time.Time) {
	if !a.lastWatchdog.IsZero() && now.Sub(a.lastWatchdog) < watchdogInterval {
		return
	}
	a.lastWatchdog = now
	a.watchdogChecks++
	for id := range a.lastInteraction {
		if _, known := a.joined[id]; !known {
			delete(a.lastInteraction, id)
		}
	}
	for id, v := range a.joined {
		v.Scale = avatarScale(now, v.JoinedAt)
		if _, active := a.viewers[id]; !active && !v.RespawnAt.IsZero() && !now.Before(v.RespawnAt) && len(a.viewers) < maxActiveViewers {
			v.X, v.Y = a.spawnPosition()
			v.VX, v.VY = randomVelocity()
			v.RespawnAt = time.Time{}
			a.viewers[id] = v
		}
		if now.Sub(v.JoinedAt) > joinedRetention && v.RespawnAt.IsZero() && a.viewers[id] == nil {
			delete(a.joined, id)
			delete(a.lastInteraction, id)
		}
	}
}

func avatarScale(now, joinedAt time.Time) float64 {
	age := now.Sub(joinedAt).Seconds()
	if age <= 0 {
		return 1
	}
	if age >= joinedRetention.Seconds() {
		return 0.45
	}
	return 1 - 0.55*(age/joinedRetention.Seconds())
}

func respawnDelay(score int) time.Duration {
	delay := 20.0 / (1 + float64(score)/5)
	if delay < 3 {
		delay = 3
	}
	return time.Duration(delay * float64(time.Second))
}

func interactionPoints(eventType string) int {
	switch eventType {
	case "gift":
		return 10
	case "follow":
		return 5
	case "comment":
		return 2
	case "like":
		return 1
	default:
		return 1
	}
}

func (a *arena) resolveCollisions() {
	viewers := make([]*viewer, 0, len(a.viewers))
	for _, v := range a.viewers {
		viewers = append(viewers, v)
	}
	removed := map[string]bool{}
	for i := 0; i < len(viewers); i++ {
		for j := i + 1; j < len(viewers); j++ {
			one, two := viewers[i], viewers[j]
			if removed[one.ID] || removed[two.ID] {
				continue
			}
			collisionDistance := 50*one.Scale + 50*two.Scale
			if collisionDistance <= 0 {
				collisionDistance = 100
			}
			dx, dy := two.X-one.X, two.Y-one.Y
			distanceSquared := dx*dx + dy*dy
			if distanceSquared >= collisionDistance*collisionDistance {
				continue
			}
			distance := math.Sqrt(distanceSquared)
			if distance < 0.001 {
				dx, dy, distance = 1, 0, 1
			}
			nx, ny := dx/distance, dy/distance
			overlap := (collisionDistance - distance) / 2
			one.X -= nx * overlap
			one.Y -= ny * overlap
			two.X += nx * overlap
			two.Y += ny * overlap

			// The newest joined portrait wins the collision. The older
			// portrait is consumed and explodes on contact.
			oneIsNewer := one.JoinedAt.After(two.JoinedAt) ||
				(one.JoinedAt.Equal(two.JoinedAt) && one.InteractionSeq > two.InteractionSeq)
			loser := two
			if !oneIsNewer {
				loser = one
			}
			a.explosions = append(a.explosions, explosion{X: loser.X, Y: loser.Y, Color: loser.Color, Started: time.Now()})
			removed[loser.ID] = true

		}
	}
	for id := range removed {
		delete(a.viewers, id)
		if v, ok := a.joined[id]; ok {
			v.RespawnAt = time.Now().Add(respawnDelay(v.InteractionScore))
		}
	}
}

func (a *arena) removeStale(now time.Time) {
	for id, v := range a.viewers {
		if now.Sub(v.LastSeen) > timeout {
			delete(a.viewers, id)
		}
	}
}

func (a *arena) removeLeastRecent() {
	var oldestID string
	var oldest time.Time
	for id, v := range a.viewers {
		if oldestID == "" || v.LastSeen.Before(oldest) {
			oldestID = id
			oldest = v.LastSeen
		}
	}
	if oldestID != "" {
		delete(a.viewers, oldestID)
	}
}

func (a *arena) spawnPosition() (float64, float64) {
	// Six columns by four rows leaves generous space for the portrait and name.
	// Pick a random free cell so new arrivals do not stack on existing viewers.
	positions := make([][2]float64, 0, maxActiveViewers)
	for row := 0; row < 4; row++ {
		for col := 0; col < 6; col++ {
			positions = append(positions, [2]float64{
				150 + float64(col)*156,
				430 + float64(row)*300,
			})
		}
	}
	for _, index := range rand.Perm(len(positions)) {
		candidate := positions[index]
		free := true
		for _, v := range a.viewers {
			dx := v.X - candidate[0]
			dy := v.Y - candidate[1]
			if dx*dx+dy*dy < viewerSlotSpacing*viewerSlotSpacing {
				free = false
				break
			}
		}
		if free {
			return candidate[0], candidate[1]
		}
	}
	return 150, 430
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
	a.drawExplosions()
	a.drawJoinBursts()
	if len(a.viewers) == 0 {
		ctx.Set("fillStyle", "rgba(255,255,255,0.6)")
		ctx.Set("font", "24px Segoe UI, system-ui, sans-serif")
		ctx.Call("fillText", "Waiting for viewers...", logicalW/2, 990)
	}
}

func (a *arena) drawExplosions() {
	now := time.Now()
	ctx := a.ctx
	for _, effect := range a.explosions {
		progress := now.Sub(effect.Started).Seconds() / 0.55
		if progress >= 1 {
			continue
		}
		radius := 35 + progress*105
		ctx.Call("save")
		ctx.Set("globalAlpha", 1-progress)
		ctx.Set("strokeStyle", effect.Color)
		ctx.Set("lineWidth", 10-6*progress)
		ctx.Call("beginPath")
		ctx.Call("arc", effect.X, effect.Y, radius, 0, 2*math.Pi)
		ctx.Call("stroke")
		ctx.Set("fillStyle", "#ffffff")
		ctx.Set("font", "900 52px Segoe UI, system-ui, sans-serif")
		ctx.Set("textAlign", "center")
		ctx.Call("fillText", "✦", effect.X, effect.Y+18)
		ctx.Call("restore")
	}
}

func (a *arena) drawJoinBursts() {
	now := time.Now()
	ctx := a.ctx
	for _, burst := range a.joinBursts {
		progress := now.Sub(burst.Started).Seconds() / 1.1
		if progress >= 1 {
			continue
		}
		ctx.Call("save")
		ctx.Set("globalAlpha", 1-progress)
		ctx.Set("strokeStyle", burst.Color)
		ctx.Set("lineWidth", 14-8*progress)
		for ring := 0; ring < 2; ring++ {
			ringProgress := math.Mod(progress+float64(ring)*0.18, 1)
			radius := 65 + ringProgress*150
			ctx.Call("beginPath")
			ctx.Call("arc", burst.X, burst.Y, radius, 0, 2*math.Pi)
			ctx.Call("stroke")
		}
		ctx.Set("fillStyle", "#ffffff")
		for particle := 0; particle < 12; particle++ {
			angle := float64(particle) * 2 * math.Pi / 12
			distance := 75 + progress*155
			x := burst.X + math.Cos(angle)*distance
			y := burst.Y + math.Sin(angle)*distance
			ctx.Call("beginPath")
			ctx.Call("arc", x, y, 7*(1-progress), 0, 2*math.Pi)
			ctx.Call("fill")
		}
		ctx.Call("restore")
	}
}

func (a *arena) drawViewer(v *viewer) {
	ctx := a.ctx
	color := v.Color
	if color == "" {
		color = "#5de1e6"
	}
	pulsing := time.Now().Before(v.PulseUntil)
	radius := 50 * v.Scale
	if radius <= 0 {
		radius = 50
	}
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
			a.joined = map[string]*viewer{}
			a.lastInteraction = map[string]time.Time{}
			a.explosions = nil
			a.joinBursts = nil
			return nil
		}
		var e struct {
			Type   string `json:"type"`
			At     string `json:"at"`
			Viewer struct {
				ID        string `json:"id"`
				Username  string `json:"username"`
				AvatarURL string `json:"avatar_url"`
				Color     string `json:"color"`
			} `json:"viewer"`
		}
		if json.Unmarshal([]byte(args[0].Get("data").String()), &e) != nil || e.Viewer.ID == "" {
			return nil
		}
		a.sequence++
		interactionAt := time.Now()
		if parsed, err := time.Parse(time.RFC3339Nano, e.At); err == nil {
			interactionAt = parsed
		}
		v := a.viewers[e.Viewer.ID]
		if v == nil {
			v = a.joined[e.Viewer.ID]
		}
		if v == nil {
			x, y := a.spawnPosition()
			vx, vy := randomVelocity()
			v = &viewer{
				ID: e.Viewer.ID, Name: e.Viewer.Username, Avatar: e.Viewer.AvatarURL,
				Color: e.Viewer.Color, X: x, Y: y, VX: vx, VY: vy,
				InteractionSeq: a.sequence, InteractionAt: interactionAt, JoinedAt: interactionAt, Scale: 1, LastSeen: time.Now(),
			}
			a.joined[v.ID] = v
			a.loadAvatar(v)
		} else if v.Avatar == "" && e.Viewer.AvatarURL != "" {
			v.Avatar = e.Viewer.AvatarURL
			a.loadAvatar(v)
		}
		v.Scale = avatarScale(time.Now(), v.JoinedAt)
		if _, active := a.viewers[v.ID]; active || len(a.viewers) < maxActiveViewers {
			a.viewers[v.ID] = v
		}
		if e.Type == "join" {
			a.joinBursts = append(a.joinBursts, joinBurst{X: v.X, Y: v.Y, Color: v.Color, Started: time.Now()})
		}
		v.InteractionSeq = a.sequence
		v.InteractionAt = interactionAt
		v.LastSeen = time.Now()
		v.InteractionScore += interactionPoints(e.Type)
		a.lastInteraction[v.ID] = v.LastSeen
		if e.Type != "join" {
			v.PulseUntil = time.Now().Add(500 * time.Millisecond)
		}
		return nil
	})
	es := js.Global().Get("EventSource").New("/api/events")
	es.Call("addEventListener", "message", a.event)
}

func randomVelocity() (float64, float64) {
	angle := rand.Float64() * 2 * math.Pi
	speed := 55 + rand.Float64()*55
	return math.Cos(angle) * speed, math.Sin(angle) * speed
}

func (a *arena) loadAvatar(v *viewer) {
	if v.Avatar == "" || v.ImageReady {
		return
	}
	img := js.Global().Get("Image").New()
	callback := js.FuncOf(func(js.Value, []js.Value) interface{} {
		v.Image = img
		v.ImageReady = true
		v.ImageFailed = false
		return nil
	})
	failure := js.FuncOf(func(js.Value, []js.Value) interface{} {
		v.ImageFailed = true
		return nil
	})
	a.imageLoad[v.ID] = callback
	a.imageLoad[v.ID+":error"] = failure
	img.Set("onload", callback)
	img.Set("onerror", failure)
	img.Set("decoding", "async")
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
