package main

// TODO?: julia set
// TODO?: ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

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
	STARTING_POINT      = complex(0, 0)

	// INITIAL_CENTER = complex(0, 1)
	// INITIAL_ZOOM   = 2.5e-13
	INITIAL_CENTER    = complex(-.75, 0)
	INITIAL_ZOOM      = 2.5e-0
	DELTA_STEP        = 1024
	MAX_STEP          = DELTA_STEP * 8
	ZOOM_DELTA        = .94
	MOUSE_WHEEL_SPEED = 1
	MOVEMENT_SPEED    = SCREEN_WIDTH / 50

	// below values should not be changed
	GAME_WIDTH  = SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT = SCREEN_HEIGHT * SQRT_DOTS_PER_PIXEL
	THRESHOLD   = float64(2)
	MIN_ZOOM    = 2.5e-13
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
	isPressed bool
	x, y      int
}

type Buffer struct {
	palette [][]byte
	updated chan bool
	content []byte
}

type ThreadStatus struct {
	nThreads     int
	idx          chan int
	canReload    chan bool
	haveToStop   chan bool
	canStart     chan bool
	workingCount sync.WaitGroup
}

type DoublePoints struct {
	zoomX    float64
	zoomY    float64
	curr     int
	dbCentre [2]complex128
	dbPoints [2][]PointStatus
}

type Game struct {
	points        DoublePoints
	movementSpeed int
	th            ThreadStatus
	keys          []ebiten.Key
	mouse         Mouse
	buf           Buffer
	image         *ebiten.Image
}

func main() {
	ebiten.SetWindowSize(SCREEN_WIDTH, SCREEN_HEIGHT)
	ebiten.SetWindowTitle("Mandelbrot set")
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
	return GAME_WIDTH, GAME_HEIGHT
}

func NewGame() *Game {
	nThreads := runtime.NumCPU()
	g := Game{
		points: DoublePoints{
			zoomX:    INITIAL_ZOOM,
			zoomY:    INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
			curr:     0,
			dbCentre: [2]complex128{INITIAL_CENTER, INITIAL_CENTER},
			dbPoints: [2][]PointStatus{
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
			},
		},
		movementSpeed: MOVEMENT_SPEED,
		th: ThreadStatus{
			nThreads:     nThreads,
			idx:          make(chan int),
			haveToStop:   make(chan bool, nThreads),
			canStart:     make(chan bool, nThreads),
			canReload:    make(chan bool, 1),
			workingCount: sync.WaitGroup{},
		},
		// keys: default,
		buf: Buffer{
			palette: NewPalette(),
			updated: make(chan bool, 1),
			content: make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		},
		image: ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		mouse: Mouse{
			isPressed: false,
			// x, y: default,
		},
	}
	g.th.workingCount.Add(g.th.nThreads)
	for i := 0; i < g.th.nThreads; i++ {
		go Worker(&g)
	}
	g.th.canReload <- true
	g.Reload()
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
		g.mouse.isPressed = true
		g.mouse.x, g.mouse.y = x, y
	case inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft):
		g.mouse.isPressed = false
	}
	if g.mouse.isPressed && (x != g.mouse.x || y != g.mouse.y) {
		g.PartiallyReload(x-g.mouse.x, y-g.mouse.y)
	} else {
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
	}
	g.mouse.x, g.mouse.y = x, y
}

func (g *Game) ReadKeyboard() error {
	g.keys = inpututil.AppendPressedKeys(g.keys[:0])
	for _, k := range g.keys {
		switch k {
		case ebiten.KeyR:
			g.points.dbCentre[g.points.curr] = INITIAL_CENTER
			g.points.zoomX = INITIAL_ZOOM
			g.points.zoomY = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			g.Reload()
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.PartiallyReload(g.movementSpeed, 0)
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.PartiallyReload(0, -g.movementSpeed)
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.PartiallyReload(0, g.movementSpeed)
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.PartiallyReload(-g.movementSpeed, 0)
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	return nil
}

func (g *Game) Zoom(zoom float64) {
	newZoom := g.points.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	g.points.zoomX = newZoom
	g.points.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	g.Reload()
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	newZoom := g.points.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	fixedPoint := complex(
		float64(g.mouse.x-GAME_WIDTH/2)*g.points.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY/GAME_HEIGHT,
	)
	g.points.zoomX = newZoom
	g.points.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	delta := fixedPoint - complex(
		float64(g.mouse.x-GAME_WIDTH/2)*g.points.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY/GAME_HEIGHT,
	)
	g.points.dbCentre[g.points.curr] += delta
	g.Reload()
}

func (g *Game) Reload() {
	g.DoTheMath(42, 42, false)
}

func (g *Game) PartiallyReload(dx, dy int) {
	g.DoTheMath(dx, dy, true)
}

func (g *Game) DoTheMath(dx, dy int, cpyFlag bool) {
	select {
	case <-g.th.canReload:
		defer func() {
			g.th.canReload <- true
		}()
	default:
		return
	}
	for cpu := 0; cpu < g.th.nThreads; cpu++ {
		g.th.haveToStop <- true
	}
	g.th.workingCount.Wait()
	defer g.th.workingCount.Add(g.th.nThreads)
	close(g.th.idx)
	g.th.idx = make(chan int, GAME_WIDTH*GAME_HEIGHT)

	if cpyFlag {
		g.points.curr = 1 - g.points.curr
		g.points.dbCentre[g.points.curr] = g.points.dbCentre[1-g.points.curr] +
			complex(float64(-dx)*g.points.zoomX/GAME_WIDTH,
				float64(dy)*g.points.zoomY/GAME_HEIGHT)
	}
	leftUp := g.points.dbCentre[g.points.curr] + complex(-g.points.zoomX/2, g.points.zoomY/2)
	deltaX := g.points.zoomX / (GAME_WIDTH - 1)
	deltaY := -g.points.zoomY / (GAME_HEIGHT - 1)

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

	for cpu := 0; cpu < g.th.nThreads; cpu = cpu + 1 {
		cpu := cpu
		go func() {
			for i := cpu; i < GAME_HEIGHT; i = i + g.th.nThreads {
				for j := 0; j < GAME_WIDTH; j++ {
					// it could be doable without the if, if tha game could reset
					// to the previous frame but I don't think it would be enjoiable
					// to not see anything drawn while dragging the mouse
					// maybe it is better a little lag
					if !cpyFlag {
						select {
						case <-g.th.haveToStop:
							g.th.workingCount.Done()
							return
						default:
						}
					}
					idx := i*GAME_WIDTH + j
					point := &g.points.dbPoints[g.points.curr][idx]
					if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
						prevIdx := (i-dy)*GAME_WIDTH + (j - dx)
						prevPoint := &g.points.dbPoints[1-g.points.curr][prevIdx]
						*point = *prevPoint
					} else {
						point.c = leftUp + complex(float64(j)*deltaX, float64(i)*deltaY)
						point.z = STARTING_POINT
						point.steps = 0
						point.finished = false
					}
					g.WriteToBuffer(idx, point.steps)
					if !point.finished {
						g.th.idx <- idx
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
		case idx := <-g.th.idx:
			point := &g.points.dbPoints[g.points.curr][idx]
			z, stepDone := EscapeSteps(point.c, point.z, DELTA_STEP)
			point.z = z
			if stepDone == -1 {
				point.steps += DELTA_STEP
				if point.steps < MAX_STEP {
					g.th.idx <- idx
				}
				continue
			}
			point.steps += stepDone
			point.finished = true
			g.WriteToBuffer(idx, point.steps)
			select {
			case g.buf.updated <- true:
			default:
			}
		}
	}
}

func EscapeSteps(c, z complex128, maxStep int) (complex128, int) {
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
