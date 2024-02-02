package main

import (
	"log"
	"runtime"
	"sync"
	// "time"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// TODO: IF MOVEMENT IN ANY DIRECTION, DON'T RE-CALCULATE ALL THE PIXELS
// TODO: LOAD FORM THE MIDDLE OF THE SCREEN, NOT FROM THE TOP (WHEN ZOOMING)

const (
	SCREEN_WIDTH        = 720
	SCREEN_HEIGHT       = 720
	SQRT_DOTS_PER_PIXEL = 2
	STARTING_POINT      = complex(0, 0)

	// CENTRE_0       = complex(-.75, 0)
	DELTA_STEP        = 1024
	MAX_STEP          = DELTA_STEP * 8
	INITIAL_ZOOM      = 2.5e-0
	ZOOM_DELTA        = .96
	MOUSE_WHEEL_SPEED = 2.0
	MOVEMENT_SPEED    = 0.01

	// max zoom
	INITIAL_CENTER = complex(-.749000099001, 0.101000001)
	// ZOOM           = 2.5e-13

	// below value should not be changed
	GAME_WIDTH  = SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT = SCREEN_HEIGHT * SQRT_DOTS_PER_PIXEL
	THRESHOLD   = float64(2)
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

type pointStatus struct {
	z, c  complex128
	steps int
}

type mouse struct {
	isPressed bool
	x, y      int
}

type Game struct {
	points        []pointStatus
	buf           []byte
	palette       [][]byte
	nThreads      int
	chIdx         chan int
	chCanReset    chan bool
	chStop        chan bool
	image         *ebiten.Image
	centre        complex128
	zoomX         float64
	zoomY         float64
	movementSpeed float64
	wg            sync.WaitGroup
	keys          []ebiten.Key
	mouse         mouse
	needUpdate    bool
}

func NewGame() *Game {
	nThreads := max(1, runtime.NumCPU()-1)
	g := Game{
		points:        make([]pointStatus, GAME_WIDTH*GAME_HEIGHT),
		buf:           make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		palette:       NewPalette(),
		nThreads:      nThreads,
		chIdx:         make(chan int, nThreads),
		chCanReset:    make(chan bool, 1),
		chStop:        make(chan bool, nThreads),
		image:         ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		centre:        INITIAL_CENTER,
		zoomX:         INITIAL_ZOOM,
		zoomY:         INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
		movementSpeed: INITIAL_ZOOM * MOVEMENT_SPEED,
		// wg:         default
		// keys:       default
		// mouse:      default
		// needUpdate: default
	}
	go func() {
		for i := 0; i < g.nThreads; i++ {
			<-g.chStop
		}
	}()
	g.chCanReset <- true
	go g.doTheMath()
	return &g
}

func worker(g *Game) {
	defer g.wg.Done()
	for {
		select {
		case <-g.chStop:
			return
		case idx := <-g.chIdx:
			select {
			case <-g.chStop:
				return
			default:
			}
			point := &g.points[idx]
			z, stepDone := DivSteps(point.c, point.z, DELTA_STEP)
			point.z = z
			if stepDone == -1 {
				point.steps += DELTA_STEP
				if point.steps < MAX_STEP {
					g.chIdx <- idx
				}
				continue
			}
			point.steps += stepDone
			s := point.steps % len(g.palette)
			g.buf[idx*4] = g.palette[s][0]
			g.buf[idx*4+1] = g.palette[s][1]
			g.buf[idx*4+2] = g.palette[s][2]
			g.buf[idx*4+3] = g.palette[s][3]
		}
	}
}

func (g *Game) doTheMath() {
	select {
	case <-g.chCanReset:
		g.needUpdate = false
	default:
		g.needUpdate = true
		return
	}
	for cpu := 0; cpu < g.nThreads; cpu++ {
		g.chStop <- true
	}
	g.wg.Wait()
	close(g.chIdx)
	g.chIdx = make(chan int, GAME_WIDTH*GAME_HEIGHT+g.nThreads)
	column := g.centre + complex(-g.zoomX/2, g.zoomY/2)
	deltaX := complex(g.zoomX/(GAME_WIDTH-1), 0)
	deltaY := complex(0, -g.zoomY/(GAME_HEIGHT-1))
	deltaCpuY := complex(float64(g.nThreads), 0) * deltaY
	deltaI := g.nThreads * GAME_WIDTH

	var localWg sync.WaitGroup
	localWg.Add(g.nThreads)
	for cpu := 0; cpu < g.nThreads; cpu, column = cpu+1, column+deltaY {
		column := column
		cpu := cpu
		go func() {
			defer localWg.Done()
			for i := cpu * GAME_WIDTH; i < GAME_WIDTH*GAME_HEIGHT; i, column = i+deltaI, column+deltaCpuY {
				c := column
				for j := i; j < i+GAME_WIDTH; j, c = j+1, c+deltaX {
					point := &g.points[j]
					point.c = c
					point.z = STARTING_POINT
					point.steps = 0
					g.buf[j*4] = g.palette[0][0]
					g.buf[j*4+1] = g.palette[0][1]
					g.buf[j*4+2] = g.palette[0][2]
					g.buf[j*4+3] = g.palette[0][3]
					g.chIdx <- j
				}
			}
		}()
	}
	localWg.Wait()
	g.chCanReset <- true

	g.wg.Add(g.nThreads)
	for cpu := 0; cpu < g.nThreads; cpu++ {
		go worker(g)
	}
}

func (g *Game) Move(delta complex128) {
	g.centre += delta
	go g.doTheMath()
}

func (g *Game) Zoom(zoom float64) {
	g.zoomX *= zoom
	g.zoomY = GAME_HEIGHT * g.zoomX / GAME_WIDTH
	g.movementSpeed = g.zoomX * 0.01
	go g.doTheMath()
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	fixedPoint := complex(float64(g.mouse.x-GAME_WIDTH/2)*g.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.zoomY/GAME_HEIGHT)

	g.zoomX *= zoom
	g.zoomY = g.zoomX * GAME_HEIGHT / GAME_WIDTH

	substitutedPoint := complex(float64(g.mouse.x-GAME_WIDTH/2)*g.zoomX/GAME_WIDTH,
		float64(-g.mouse.y+GAME_HEIGHT/2)*g.zoomY/GAME_HEIGHT)
	delta := fixedPoint - substitutedPoint
	g.centre += delta

	g.movementSpeed = g.zoomX * 0.01
	go g.doTheMath()
}

func (g *Game) UpdateMouse() {
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		g.mouse.isPressed = true
		x, y := ebiten.CursorPosition()
		g.mouse.x, g.mouse.y = x, y
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		g.mouse.isPressed = false
	}
	x, y := ebiten.CursorPosition()
	if g.mouse.isPressed && (x != g.mouse.x || y != g.mouse.y) {
		delta := complex(float64(g.mouse.x-x)*g.zoomX/GAME_WIDTH,
			float64(y-g.mouse.y)*g.zoomY/GAME_HEIGHT)
		g.Move(delta)
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

func (g *Game) Update() error {
	g.UpdateMouse()
	g.keys = inpututil.AppendPressedKeys(g.keys[:0])
	// if ebiten.IsKeyPressed(ebiten.KeyEscape) {
	//return ebiten.Termination
	// }
	for _, k := range g.keys {
		switch k {
		case ebiten.KeyR:
			g.centre = INITIAL_CENTER
			g.zoomX = INITIAL_ZOOM
			g.zoomY = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			g.movementSpeed = INITIAL_ZOOM * 0.01
			go g.doTheMath()
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.Move(complex(-g.movementSpeed, 0))
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.Move(complex(0, -g.movementSpeed))
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.Move(complex(0, g.movementSpeed))
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.Move(complex(g.movementSpeed, 0))
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	g.image.WritePixels(g.buf)
	if g.needUpdate {
		go g.doTheMath()
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
