package main

import (
	"log"
	"runtime"
	"sync"
	// "time"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// TODO: load form the middle of the screen, not from the top
//		- precalculate the order of rendered pixel at the start of the program:
//			- start from the center
//			- add 1 to the radius, draw the topmost and bottommost pixels
//			- for every row, start from the leftmost pixel until you reach an
//			already set pixel
//			- to the same thing starting from the rightmost pixel
//			- repeat

const (
	SCREEN_WIDTH        = 720
	SCREEN_HEIGHT       = 720
	SQRT_DOTS_PER_PIXEL = 2
	STARTING_POINT      = complex(0, 0)

	INITIAL_CENTER    = complex(-.75, 0)
	DELTA_STEP        = 1024
	MAX_STEP          = DELTA_STEP * 8
	INITIAL_ZOOM      = 2.5e-0
	ZOOM_DELTA        = .95
	MOUSE_WHEEL_SPEED = 1
	MOVEMENT_SPEED    = SCREEN_WIDTH / 100

	// below values should not be changed
	GAME_WIDTH  = SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT = SCREEN_HEIGHT * SQRT_DOTS_PER_PIXEL
	THRESHOLD   = float64(2)
	MIN_ZOOM    = 2.5e-13
)

// Wikipedia palette
var (
	pre_palette [64]byte = [64]byte{
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

// returns the int -1 if the point is still in the set
func DivSteps(c, z complex128, maxStep int) (complex128, int) {
	for i := 0; i < maxStep; i++ {
		z = z*z + c
		if real(z)*real(z)+imag(z)*imag(z) > THRESHOLD*THRESHOLD {
			return z, i
		}
	}
	return z, -1
}

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
	finishedThread sync.WaitGroup
	idx            chan int
	canReset       chan bool
	stop           chan bool
	bufChaged      chan bool
}

type PositionInfo struct {
	centre complex128
	zoomX  float64
	zoomY  float64
}

type DoublePoints struct {
	dbPoints [2][]PointStatus
	curr     int
}

type Game struct {
	palette       [][]byte
	keys          []ebiten.Key
	buf           []byte
	pp            DoublePoints
	mouse         Mouse
	th            ThreadStatus
	pos           PositionInfo
	image         *ebiten.Image
	movementSpeed int
}

func NewGame() *Game {
	nThreads := runtime.NumCPU()
	g := Game{
		pp: DoublePoints{
			dbPoints: [2][]PointStatus{
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
				make([]PointStatus, GAME_WIDTH*GAME_HEIGHT),
			},
			curr: 0,
		},
		buf:     make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		palette: NewPalette(),
		image:   ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		// keys:       default
		// mouse:      default
		th: ThreadStatus{
			nThreads:   nThreads,
			idx:        make(chan int, nThreads),
			stop:       make(chan bool, nThreads),
			canReset:   make(chan bool, 1),
			bufChaged:  make(chan bool, 1),
			// wg         default
		},
		pos: PositionInfo{
			centre: INITIAL_CENTER,
			zoomX:  INITIAL_ZOOM,
			zoomY:  INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
		},
		movementSpeed: MOVEMENT_SPEED,
	}
	go func() {
		for i := 0; i < g.th.nThreads; i++ {
			<-g.th.stop
		}
	}()
	g.th.canReset <- true
	g.doTheMath()
	return &g
}

func worker(g *Game) {
	for {
		select {
		case <-g.th.stop:
			return
		case idx := <-g.th.idx:
			point := &g.pp.dbPoints[g.pp.curr][idx]
			z, stepDone := DivSteps(point.c, point.z, DELTA_STEP)
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
			g.writeToBuffer(idx, point.steps)
			select {
			case g.th.bufChaged <- true:
			default:
			}
		}
	}
}

func (g *Game) writeToBuffer(idx, s int) {
	buf := &g.buf
	pal := &g.palette[s%len(g.palette)]
	idx *= 4
	(*buf)[idx] = (*pal)[0]
	(*buf)[idx+1] = (*pal)[1]
	(*buf)[idx+2] = (*pal)[2]
	(*buf)[idx+3] = (*pal)[3]
}

func (g *Game) doTheMath() {
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

	g.th.idx = make(chan int, GAME_WIDTH*GAME_HEIGHT+g.th.nThreads)
	leftPoint := g.pos.centre + complex(-g.pos.zoomX/2, g.pos.zoomY/2)
	deltaX := complex(g.pos.zoomX/(GAME_WIDTH-1), 0)
	deltaY := complex(0, -g.pos.zoomY/(GAME_HEIGHT-1))
	deltaCpuY := complex(float64(g.th.nThreads), 0) * deltaY

	for cpu := 0; cpu < g.th.nThreads; cpu, leftPoint = cpu+1, leftPoint+deltaY {
		leftPoint := leftPoint
		cpu := cpu
		go func() {
			defer g.th.finishedThread.Done()
			for i := cpu; i < GAME_HEIGHT; i, leftPoint = i+g.th.nThreads, leftPoint+deltaCpuY {
				c := leftPoint
				for j := 0; j < GAME_WIDTH; j, c = j+1, c+deltaX {
					select {
					case <-g.th.stop:
						return
					default:
					}
					idx := i*GAME_WIDTH + j
					point := &g.pp.dbPoints[g.pp.curr][idx]
					point.c = c
					point.z = STARTING_POINT
					point.steps = 0
					point.finished = false
					g.writeToBuffer(idx, point.steps)
					g.th.idx <- idx
				}
			}
			worker(g)
		}()
	}
}

func (g *Game) Move(dx, dy int) {
	delta := complex(float64(-dx)*g.pos.zoomX/GAME_WIDTH,
		float64(dy)*g.pos.zoomY/GAME_HEIGHT)
	g.pos.centre += delta
	g.copyThenDoTheMath(dx, dy)
}

func (g *Game) Zoom(zoom float64) {
	newZoom := g.pos.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	g.pos.zoomX = newZoom
	g.pos.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH
	g.doTheMath()
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	newZoom := g.pos.zoomX * zoom
	if newZoom < MIN_ZOOM {
		return
	}
	fixedPoint := complex(float64(g.mouse.x-GAME_WIDTH/2)*g.pos.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.pos.zoomY/GAME_HEIGHT)

	g.pos.zoomX = newZoom
	g.pos.zoomY = newZoom * GAME_HEIGHT / GAME_WIDTH

	substitutedPoint := complex(float64(g.mouse.x-GAME_WIDTH/2)*g.pos.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.pos.zoomY/GAME_HEIGHT)
	delta := fixedPoint - substitutedPoint
	g.pos.centre += delta

	g.doTheMath()
}

func (g *Game) UpdateMouse() {
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
		g.Move(x-g.mouse.x, y-g.mouse.y)
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

func (g *Game) UpdateKeyboard() error {
	g.keys = inpututil.AppendPressedKeys(g.keys[:0])
	for _, k := range g.keys {
		switch k {
		case ebiten.KeyR:
			g.pos.centre = INITIAL_CENTER
			g.pos.zoomX = INITIAL_ZOOM
			g.pos.zoomY = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			g.doTheMath()
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.Move(-g.movementSpeed, 0)
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.Move(0, -g.movementSpeed)
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.Move(0, g.movementSpeed)
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.Move(g.movementSpeed, 0)
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	return nil
}

func (g *Game) Update() error {
	g.UpdateMouse()
	err := g.UpdateKeyboard()
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

func main() {
	ebiten.SetWindowSize(SCREEN_WIDTH, SCREEN_HEIGHT)
	ebiten.SetWindowTitle("Mandelbrot set")
	if err := ebiten.RunGame(NewGame()); err != nil {
		log.Fatal(err)
	}
}

func (g *Game) copyThenDoTheMath(dx, dy int) {
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

	g.th.idx = make(chan int, GAME_WIDTH*GAME_HEIGHT+g.th.nThreads)
	leftPoint := g.pos.centre + complex(-g.pos.zoomX/2, g.pos.zoomY/2)
	deltaX := complex(g.pos.zoomX/(GAME_WIDTH-1), 0)
	deltaY := complex(0, -g.pos.zoomY/(GAME_HEIGHT-1))
	deltaCpuY := complex(float64(g.th.nThreads), 0) * deltaY

	var xStart, yStart, xEnd, yEnd int
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

	g.pp.curr = 1 - g.pp.curr
	for cpu := 0; cpu < g.th.nThreads; cpu, leftPoint = cpu+1, leftPoint+deltaY {
		leftPoint := leftPoint
		cpu := cpu
		go func() {
			defer g.th.finishedThread.Done()
			for i := cpu; i < GAME_HEIGHT; i, leftPoint = i+g.th.nThreads, leftPoint+deltaCpuY {
				c := leftPoint
				for j := 0; j < GAME_WIDTH; j, c = j+1, c+deltaX {
					idx := i*GAME_WIDTH + j
					point := &g.pp.dbPoints[g.pp.curr][idx]
					if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
						prevIdx := (i-dy)*GAME_WIDTH + (j - dx)
						prevPoint := &g.pp.dbPoints[1-g.pp.curr][prevIdx]
						*point = *prevPoint
					} else {
						point.c = c
						point.z = STARTING_POINT
						point.steps = 0
						point.finished = false
					}
					g.writeToBuffer(idx, point.steps)
					if !point.finished {
						g.th.idx <- idx
					}
				}
			}
			worker(g)
		}()
	}
}
