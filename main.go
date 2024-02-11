package main

// @todo? ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
// @todo  blending (do i need to do all the calculations even if the point
//        diverges?)

import (
	"log"
	"runtime"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	SCREEN_WIDTH        = 720
	SCREEN_HEIGHT       = 720
	SQRT_DOTS_PER_PIXEL = 2

	STARTING_POINT_0 = complex(0, 0)
	INITIAL_CENTER_0 = complex(0, 0)
	INITIAL_ZOOM_0   = 4
	// INITIAL_CENTER_0 = complex(0, 1)
	// INITIAL_ZOOM_0   = 2.5e-13

	STARTING_POINT_1 = complex(0, 0)
	INITIAL_ZOOM_1   = 4
	INITIAL_CENTER_1 = complex(0, 0)

	DELTA_STEP        = 1024
	MAX_STEP          = DELTA_STEP * 8
	ZOOM_DELTA        = .9
	MOUSE_WHEEL_SPEED = 1.2
	MOVEMENT_SPEED    = SCREEN_WIDTH / 50

	// below values should not be changed
	DB_SCREEN_WIDTH = 720 * 2
	DB_GAME_WIDTH   = DB_SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_WIDTH      = SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT     = SCREEN_HEIGHT * SQRT_DOTS_PER_PIXEL
	THRESHOLD       = float64(2)
	MIN_ZOOM        = 2.5e-13
)

// Wikipedia palette
var (
	prePalette []byte = []byte{
		9, 1, 47, 0xff,
		4, 4, 73, 0xff,
		0, 7, 100, 0xff,
		12, 44, 138, 0xff,
		24, 82, 177, 0xff,
		57, 125, 209, 0xff,
		134, 181, 229, 0xff,
		211, 236, 248, 0xff,
		241, 233, 191, 0xff,
		248, 201, 95, 0xff,
		255, 170, 0, 0xff,
		204, 128, 0, 0xff,
		153, 87, 0, 0xff,
		106, 52, 3, 0xff,
		66, 30, 15, 0xff,
		25, 7, 26, 0xff,
	}
)

type PointStatus struct {
	z, c     complex128
	steps    int
	finished bool
}

type Mouse struct {
	isPressedLeft  bool
	isPressedRight bool
	x, y           int
}

type Buffer struct {
	palette [][]byte
	updated chan bool
	content []byte
}

type ThreadStatus struct {
	nThreads     int
	idx          [2]chan int
	canReload    [2]chan bool
	haveToStop   chan bool
	canStart     chan bool
	workingCount sync.WaitGroup
}

type DoublePoints struct {
	zoomX    float64
	zoomY    float64
	curr     int
	dbCentre [2]complex128
	start    complex128
}

type Game struct {
	points        [2]DoublePoints
	movementSpeed int
	th            ThreadStatus
	keys          []ebiten.Key
	mouse         Mouse
	buf           Buffer
	currentScreen int
	image         *ebiten.Image
	dbPoints      [2][]PointStatus
}

func main() {
	ebiten.SetWindowSize(DB_SCREEN_WIDTH, SCREEN_HEIGHT)
	ebiten.SetWindowTitle("Mandelbrot and Julia set")
	if err := ebiten.RunGame(NewGame()); err != nil {
		log.Fatal(err)
	}
}

func (g *Game) Update() error {
	g.ReadMouse()
	err := g.ReadKeyboard()
	if err != nil {
		return err
	}
	select {
	case <-g.buf.updated:
		g.image.WritePixels(g.buf.content)
	default:
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.image, nil)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return DB_GAME_WIDTH, GAME_HEIGHT
}

func NewGame() *Game {
	nThreads := runtime.NumCPU()
	g := Game{
		points: [2]DoublePoints{{
			zoomX:    INITIAL_ZOOM_0,
			zoomY:    INITIAL_ZOOM_0 * GAME_HEIGHT / GAME_WIDTH,
			start:    STARTING_POINT_0,
			curr:     0,
			dbCentre: [2]complex128{INITIAL_CENTER_0, INITIAL_CENTER_0},
		}, {
			zoomX:    INITIAL_ZOOM_1,
			zoomY:    INITIAL_ZOOM_1 * GAME_HEIGHT / GAME_WIDTH,
			start:    STARTING_POINT_1,
			curr:     0,
			dbCentre: [2]complex128{INITIAL_CENTER_1, INITIAL_CENTER_1},
		}},
		movementSpeed: MOVEMENT_SPEED,
		th: ThreadStatus{
			nThreads:     nThreads,
			idx:          [2]chan int{make(chan int), make(chan int)},
			haveToStop:   make(chan bool, nThreads),
			canStart:     make(chan bool, nThreads),
			canReload:    [2]chan bool{make(chan bool, 1), make(chan bool, 1)},
			workingCount: sync.WaitGroup{},
		},
		dbPoints: [2][]PointStatus{
			make([]PointStatus, DB_GAME_WIDTH*GAME_HEIGHT),
			make([]PointStatus, DB_GAME_WIDTH*GAME_HEIGHT),
		},
		// keys: default,
		buf: Buffer{
			palette: NewPalette(),
			updated: make(chan bool, 1),
			content: make([]byte, DB_GAME_WIDTH*GAME_HEIGHT*4),
		},
		image: ebiten.NewImage(DB_GAME_WIDTH, GAME_HEIGHT),
		mouse: Mouse{
			// defalut
		},
	}
	g.th.workingCount.Add(g.th.nThreads)
	for i := 0; i < g.th.nThreads; i++ {
		go Worker(&g)
	}
	g.th.canReload[1] <- true
	g.currentScreen = 1
	g.Reload(g.currentScreen)
	g.th.canReload[0] <- true
	g.currentScreen = 0
	g.Reload(g.currentScreen)
	return &g
}

func NewPalette() [][]byte {
	palette := make([][]byte, len(prePalette)/4)
	for i := 0; i < len(prePalette)/4; i++ {
		palette[i] = make([]byte, 4)
		palette[i][0] = prePalette[i*4]
		palette[i][1] = prePalette[i*4+1]
		palette[i][2] = prePalette[i*4+2]
		palette[i][3] = prePalette[i*4+3]
	}
	return palette
}

func (g *Game) ReadMouse() {
	x, y := ebiten.CursorPosition()
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		g.mouse.isPressedLeft = true
	case inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft):
		g.mouse.isPressedLeft = false
	}
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight):
		g.mouse.isPressedRight = true
	case inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonRight):
		g.mouse.isPressedRight = false
	}
	if !g.mouse.isPressedLeft && !g.mouse.isPressedRight {
		if x > GAME_WIDTH {
			g.currentScreen = 1
		} else {
			g.currentScreen = 0
		}
	}
	if g.mouse.isPressedLeft && (x != g.mouse.x || y != g.mouse.y) {
		g.PartiallyReload(g.currentScreen, x-g.mouse.x, y-g.mouse.y)
	}
	if g.mouse.isPressedRight {
		mousePoint := complex(
			float64(x-(GAME_WIDTH)/2-DisplacementX(g.currentScreen))*g.points[0].zoomX/GAME_WIDTH,
			float64(-y+GAME_HEIGHT/2)*g.points[0].zoomY/GAME_HEIGHT,
		)
		mousePoint += g.points[g.currentScreen].dbCentre[g.points[g.currentScreen].curr]
		if mousePoint != g.points[1-g.currentScreen].start {
			g.points[1-g.currentScreen].start = mousePoint
			g.Reload(1 - g.currentScreen)
		}
	}
	_, dy := ebiten.Wheel()
	if dy != 0 {
		var zoom float64
		if dy > 0 {
			zoom = ZOOM_DELTA / MOUSE_WHEEL_SPEED
		} else {
			zoom = MOUSE_WHEEL_SPEED / ZOOM_DELTA
		}
		g.ZoomFixedMouse(zoom, x, y)
	}
	g.mouse.x, g.mouse.y = x, y
}

func (g *Game) ReadKeyboard() error {
	g.keys = inpututil.AppendPressedKeys(g.keys[:0])
	cs := g.currentScreen
	for _, k := range g.keys {
		switch k {
		case ebiten.KeyE:
			if cs == 0 {
				g.points[cs].start = STARTING_POINT_0
				g.points[cs].dbCentre[g.points[cs].curr] = INITIAL_CENTER_0
				g.points[cs].zoomX = INITIAL_ZOOM_0
				g.points[cs].zoomY = GAME_HEIGHT * INITIAL_ZOOM_0 / GAME_WIDTH
			} else {
				g.points[cs].start = STARTING_POINT_1
				g.points[cs].dbCentre[g.points[cs].curr] = INITIAL_CENTER_1
				g.points[cs].zoomX = INITIAL_ZOOM_1
				g.points[cs].zoomY = GAME_HEIGHT * INITIAL_ZOOM_1 / GAME_WIDTH
			}
			g.Reload(cs)
		case ebiten.KeyR:
			if cs == 0 {
				g.points[cs].dbCentre[g.points[cs].curr] = INITIAL_CENTER_0
				g.points[cs].zoomX = INITIAL_ZOOM_0
				g.points[cs].zoomY = GAME_HEIGHT * INITIAL_ZOOM_0 / GAME_WIDTH
			} else {
				g.points[cs].dbCentre[g.points[cs].curr] = INITIAL_CENTER_1
				g.points[cs].zoomX = INITIAL_ZOOM_1
				g.points[cs].zoomY = GAME_HEIGHT * INITIAL_ZOOM_1 / GAME_WIDTH
			}
			g.Reload(cs)
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.PartiallyReload(cs, g.movementSpeed, 0)
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.PartiallyReload(cs, 0, -g.movementSpeed)
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.PartiallyReload(cs, 0, g.movementSpeed)
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.PartiallyReload(cs, -g.movementSpeed, 0)
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	return nil
}

func (g *Game) Zoom(zoom float64) {
	cs := g.currentScreen
	newZoom := g.points[cs].zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	g.points[cs].zoomX = newZoom
	g.points[cs].zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	g.Reload(g.currentScreen)
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	displacement := DisplacementX(g.currentScreen)
	cs := g.currentScreen
	newZoom := g.points[cs].zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	fixedPoint := complex(
		float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points[cs].zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.points[cs].zoomY/GAME_HEIGHT,
	)
	g.points[cs].zoomX = newZoom
	g.points[cs].zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	delta := fixedPoint - complex(
		float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points[cs].zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.points[cs].zoomY/GAME_HEIGHT,
	)
	g.points[cs].dbCentre[g.points[cs].curr] += delta
	g.Reload(g.currentScreen)
}

func (g *Game) Reload(cs int) {
	g.DoTheMath(cs, 42, 42, false)
}

func (g *Game) PartiallyReload(cs int, dx, dy int) {
	g.DoTheMath(cs, dx, dy, true)
}

func (g *Game) DoTheMath(cs int, dx, dy int, cpyFlag bool) {
	select {
	case <-g.th.canReload[cs]:
		defer func() {
			g.th.canReload[cs] <- true
		}()
	default:
		return
	}
	for cpu := 0; cpu < g.th.nThreads; cpu++ {
		g.th.haveToStop <- true
	}
	g.th.workingCount.Wait()
	defer g.th.workingCount.Add(g.th.nThreads)
	close(g.th.idx[cs])
	g.th.idx[cs] = make(chan int, GAME_WIDTH*GAME_HEIGHT)

	var leftUp complex128
	var deltaX float64
	var deltaY float64
	if cpyFlag {
		g.points[cs].curr = 1 - g.points[cs].curr
		g.points[cs].dbCentre[g.points[cs].curr] = g.points[cs].dbCentre[1-g.points[cs].curr] +
			complex(float64(-dx)*g.points[cs].zoomX/GAME_WIDTH,
				float64(dy)*g.points[cs].zoomY/GAME_HEIGHT)
	}
	leftUp = g.points[cs].dbCentre[g.points[cs].curr] + complex(-g.points[cs].zoomX/2, g.points[cs].zoomY/2)
	deltaX = g.points[cs].zoomX / (GAME_WIDTH - 1)
	deltaY = -g.points[cs].zoomY / (GAME_HEIGHT - 1)

	var xStart, yStart, xEnd, yEnd int
	if cpyFlag {
		if dx > 0 {
			xStart = dx
			xEnd = GAME_WIDTH
		} else {
			xStart = 0
			xEnd = GAME_WIDTH + dx
		}
		if dy > 0 {
			yStart = dy
			yEnd = GAME_HEIGHT
		} else {
			yStart = 0
			yEnd = GAME_HEIGHT + dy
		}
	}

	displacement := DisplacementX(cs)
	for cpu := 0; cpu < g.th.nThreads; cpu = cpu + 1 {
		minI := cpu
		go func() {
			for i := minI; i < GAME_HEIGHT; i = i + g.th.nThreads {
				for j := 0; j < GAME_WIDTH; j++ {
					// @speed can reload instantly? Less lag
					idx := i*DB_GAME_WIDTH + j + displacement
					point := &g.dbPoints[g.points[cs].curr][idx]
					if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
						prevIdx := (i-dy)*DB_GAME_WIDTH + (j - dx) + displacement
						prevPoint := &g.dbPoints[1-g.points[cs].curr][prevIdx]
						*point = *prevPoint
					} else {
						point.c = leftUp + complex(float64(j)*deltaX, float64(i)*deltaY)
						point.z = g.points[cs].start
						if cs == 1 {
							point.c, point.z = point.z, point.c
						}
						point.steps = 0
						point.finished = false
					}
					g.WriteToBuffer(idx, point.steps)
					if !point.finished {
						g.th.idx[cs] <- idx
					}
				}
			}
			g.th.canStart <- true
		}()
	}
}

func Worker(g *Game) {
	for {
		select {
		case <-g.th.haveToStop:
			g.th.workingCount.Done()
			<-g.th.canStart
		case idx := <-g.th.idx[0]:
			g.updatePoint(idx, 0)
		case idx := <-g.th.idx[1]:
			g.updatePoint(idx, 1)
		}
	}
}

func (g *Game) updatePoint(idx int, cs int) {
	point := &g.dbPoints[g.points[cs].curr][idx]
	z, stepDone := EscapeSteps(point.z, point.c, DELTA_STEP)
	point.z = z
	if stepDone == -1 {
		point.steps += DELTA_STEP
		if point.steps < MAX_STEP {
			g.th.idx[cs] <- idx
		}
		return
	}
	point.steps += stepDone
	point.finished = true
	g.WriteToBuffer(idx, point.steps)
	select {
	case g.buf.updated <- true:
	default:
	}
}

func EscapeSteps(z, c complex128, maxStep int) (complex128, int) {
	for i := 0; i < maxStep; i++ {
		z = z*z + c
		if real(z)*real(z)+imag(z)*imag(z) > THRESHOLD*THRESHOLD {
			return z, i
		}
	}
	return z, -1
}

func (g *Game) WriteToBuffer(idx, s int) {
	content := &g.buf.content
	pal := &g.buf.palette[s%len(g.buf.palette)]
	idx *= 4
	(*content)[idx] = (*pal)[0]
	(*content)[idx+1] = (*pal)[1]
	(*content)[idx+2] = (*pal)[2]
	(*content)[idx+3] = (*pal)[3]
}

func DisplacementX(cs int) int {
	if cs != 0 {
		return GAME_WIDTH
	}
	return 0
}
