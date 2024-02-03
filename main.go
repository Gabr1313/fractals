package main

// TODO?: julia set

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
	pre_palette []byte = []byte{
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

type ThreadStatus struct {
	nThreads       int
	idx            chan int
	canReset       chan bool
	stop           chan bool
	bufChaged      chan bool
	finishedThread sync.WaitGroup
}

type DoublePoints struct {
	zoomX    float64
	zoomY    float64
	curr     int
	centre   [2]complex128
	dbPoints [2][]PointStatus
}

type Game struct {
	pp            DoublePoints
	movementSpeed int
	th            ThreadStatus
	keys          []ebiten.Key
	mouse         Mouse
	palette       [][]byte
	buf           []byte
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
	case <-g.th.bufChaged:
		g.image.WritePixels(g.buf)
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
		pp: DoublePoints{
			zoomX:  INITIAL_ZOOM,
			zoomY:  INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
			curr:   0,
			centre: [2]complex128{INITIAL_CENTER, INITIAL_CENTER},
			dbPoints: [2][]PointStatus{
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
			},
		},
		movementSpeed: MOVEMENT_SPEED,
		th: ThreadStatus{
			nThreads:  nThreads,
			idx:       make(chan int, nThreads),
			stop:      make(chan bool, nThreads),
			canReset:  make(chan bool, 1),
			bufChaged: make(chan bool, 1),
			// wg         default
		},
		palette: NewPalette(),
		buf:     make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		image:   ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		// keys:       default
		// mouse:      default
	}
	go func() {
		for i := 0; i < g.th.nThreads; i++ {
			<-g.th.stop
		}
	}()
	g.th.canReset <- true
	g.DoTheMath()
	return &g
}

func NewPalette() [][]byte {
	palette := make([][]byte, len(pre_palette)/4)
	for i := 0; i < len(pre_palette)/4; i++ {
		palette[i] = make([]byte, 4)
		palette[i][0] = pre_palette[i*4]
		palette[i][1] = pre_palette[i*4+1]
		palette[i][2] = pre_palette[i*4+2]
		palette[i][3] = pre_palette[i*4+3]
	}
	return palette
}

func (g *Game) ReadMouse() {
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		g.mouse.isPressed = true
		x, y := ebiten.CursorPosition()
		g.mouse.x, g.mouse.y = x, y
	case inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft):
		g.mouse.isPressed = false
	}
	x, y := ebiten.CursorPosition()
	if g.mouse.isPressed && (x != g.mouse.x || y != g.mouse.y) {
		g.CpyDoTheMath(x-g.mouse.x, y-g.mouse.y)
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
			g.pp.centre[g.pp.curr] = INITIAL_CENTER
			g.pp.zoomX = INITIAL_ZOOM
			g.pp.zoomY = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			g.DoTheMath()
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.CpyDoTheMath(g.movementSpeed, 0)
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.CpyDoTheMath(0, -g.movementSpeed)
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.CpyDoTheMath(0, g.movementSpeed)
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.CpyDoTheMath(-g.movementSpeed, 0)
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	return nil
}

func (g *Game) Zoom(zoom float64) {
	newZoom := g.pp.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	g.pp.zoomX = newZoom
	g.pp.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	g.DoTheMath()
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	newZoom := g.pp.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	fixedPoint := complex(
		float64(g.mouse.x-GAME_WIDTH/2)*g.pp.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.pp.zoomY/GAME_HEIGHT,
	)
	g.pp.zoomX = newZoom
	g.pp.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	delta := fixedPoint - complex(
		float64(g.mouse.x-GAME_WIDTH/2)*g.pp.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.pp.zoomY/GAME_HEIGHT,
	)
	g.pp.centre[g.pp.curr] += delta
	g.DoTheMath()
}

func (g *Game) DoTheMath() {
	g._DoTheMath(false, 42, 42)
}

func (g *Game) CpyDoTheMath(dx, dy int) {
	g._DoTheMath(true, dx, dy)
}

func (g *Game) _DoTheMath(cpy bool, dx, dy int) {
	select {
	case <-g.th.canReset:
	default:
		return
	}
	for cpu := 0; cpu < g.th.nThreads; cpu++ {
		g.th.stop <- true
	}
	g.th.finishedThread.Wait()
	close(g.th.idx)
	g.th.finishedThread.Add(g.th.nThreads)
	g.th.canReset <- true

	if cpy {
		g.pp.curr = 1 - g.pp.curr
		g.pp.centre[g.pp.curr] = g.pp.centre[1-g.pp.curr] +
			complex(float64(-dx)*g.pp.zoomX/GAME_WIDTH,
				float64(dy)*g.pp.zoomY/GAME_HEIGHT)
	}
	leftUp := g.pp.centre[g.pp.curr] + complex(-g.pp.zoomX/2, g.pp.zoomY/2)
	deltaX := g.pp.zoomX / (GAME_WIDTH - 1)
	deltaY := -g.pp.zoomY / (GAME_HEIGHT - 1)

	var xStart, yStart, xEnd, yEnd int
	if cpy {
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

	g.th.idx = make(chan int, GAME_WIDTH*GAME_HEIGHT+g.th.nThreads)
	for cpu := 0; cpu < g.th.nThreads; cpu = cpu + 1 {
		cpu := cpu
		go func() {
			defer g.th.finishedThread.Done()
			for i := cpu; i < GAME_HEIGHT; i = i + g.th.nThreads {
				for j := 0; j < GAME_WIDTH; j++ {
					// it could be doable without the if, if I could reset to
					// the previous frame but I don't think it would be enjoiable
					if !cpy {
						select {
						case <-g.th.stop:
							return
						default:
						}
					}
					idx := i*GAME_WIDTH + j
					point := &g.pp.dbPoints[g.pp.curr][idx]
					if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
						prevIdx := (i-dy)*GAME_WIDTH + (j - dx)
						prevPoint := &g.pp.dbPoints[1-g.pp.curr][prevIdx]
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
			Worker(g)
		}()
	}
}

func Worker(g *Game) {
	for {
		select {
		case <-g.th.stop:
			return
		case idx := <-g.th.idx:
			point := &g.pp.dbPoints[g.pp.curr][idx]
			z, stepDone := EscapeStep(point.c, point.z, DELTA_STEP)
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
			case g.th.bufChaged <- true:
			default:
			}
		}
	}
}

func EscapeStep(c, z complex128, maxStep int) (complex128, int) {
	for i := 0; i < maxStep; i++ {
		z = z*z + c
		if real(z)*real(z)+imag(z)*imag(z) > THRESHOLD*THRESHOLD {
			return z, i
		}
	}
	return z, -1
}

func (g *Game) WriteToBuffer(idx, s int) {
	buf := &g.buf
	pal := &g.palette[s%len(g.palette)]
	idx *= 4
	(*buf)[idx] = (*pal)[0]
	(*buf)[idx+1] = (*pal)[1]
	(*buf)[idx+2] = (*pal)[2]
	(*buf)[idx+3] = (*pal)[3]
}
