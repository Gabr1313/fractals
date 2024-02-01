package main

import (
	"log"
	"runtime"
	"sync"
	// "time"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// TODO: MOUSE GESTURE
// TODO: LOAD FORM THE MIDDLE OF THE SCREEN, NOT FROM THE TOP

const (
	SCREEN_WIDTH        = 720
	SCREEN_HEIGHT       = 720
	SQRT_DOTS_PER_PIXEL = 2
	STARTING_POINT      = complex(0, 0)

	// CENTRE_0       = complex(-.75, 0)
	DELTA_STEP     = 1024
	MAX_STEP       = DELTA_STEP * 8
	INITIAL_ZOOM   = 2.5e-0
	ZOOM_DELTA     = .96
	MOVEMENT_SPEED = 0.01

	// max zoom
	INITIAL_CENTER = complex(-.749000099001, 0.101000001)
	// ZOOM           = 2.5e-13

	// below value should not be changed
	GAME_WIDTH     = SCREEN_WIDTH * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT    = SCREEN_HEIGHT * SQRT_DOTS_PER_PIXEL
	THRESHOLD      = float64(2)
	ZOOM_DELTA_REV = 1 / ZOOM_DELTA
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

type Game struct {
	points     []pointStatus
	buf        []byte
	palette    [][]byte
	nThreads   int
	chIdx      chan int
	chCanReset chan bool
	chStop     chan bool
	image      *ebiten.Image
	wg         sync.WaitGroup
	keys       []ebiten.Key
	centre     complex128
	zoomX      float64
	zoomY      float64
	movement   float64
}

func NewGame() *Game {
	nThreads := runtime.NumCPU()
	g := Game{
		points:     make([]pointStatus, GAME_WIDTH*GAME_HEIGHT),
		buf:        make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		palette:    NewPalette(),
		nThreads:   nThreads,
		chIdx:      make(chan int, nThreads),
		chCanReset: make(chan bool, 1),
		chStop:     make(chan bool, nThreads),
		image:      ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		centre:     INITIAL_CENTER,
		zoomX:      INITIAL_ZOOM,
		zoomY:      INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
		movement:   INITIAL_ZOOM * MOVEMENT_SPEED,
		// wg:     default
		// keys:     default
	}
	go func() {
		for i := 0; i < g.nThreads; i++ {
			<-g.chStop
		}
	}()
	g.chCanReset <- true
	go g.ResetGame()
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

func (g *Game) ResetGame() {
	select {
	case <-g.chCanReset:
	default:
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

func (g *Game) Move(direction complex128) {
	g.centre += direction
	go g.ResetGame()
}

func (g *Game) Zoom(zoom float64) {
	g.zoomX *= zoom
	g.zoomY = GAME_HEIGHT * g.zoomX / GAME_WIDTH
	g.movement = g.zoomX * 0.01
	go g.ResetGame()
}

func (g *Game) Update() error {
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
			g.movement = INITIAL_ZOOM * 0.01
			go g.ResetGame()
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.Move(complex(-g.movement, 0))
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.Move(complex(0, -g.movement))
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.Move(complex(0, g.movement))
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.Move(complex(g.movement, 0))
		case ebiten.KeyF, ebiten.KeySpace:
			log.Print("zoom")
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(ZOOM_DELTA_REV)
		}
	}
	g.image.WritePixels(g.buf)
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
