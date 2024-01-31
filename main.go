package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"log"
	"runtime"
	"sync"
	"time"
)

const (
	SCREEN_WIDTH        = 1080
	SCREEN_HEIGHT       = 1080
	SQRT_DOTS_PER_PIXEL = 2
	MAX_STEP            = 128
	STEP_PER_FRAME      = 32
	STARTING_POINT      = complex(0, 0)
	CENTRE              = complex(-.75, 0)
	ZOOM_X              = float64(2.5e-0)
	// CENTRE              = complex(-.749000099001, 0.101000001)
	// ZOOM_X              = float64(2.5e-13) // max zoom

	// below value should not be changed
	GAME_WIDTH  = 1080 * SQRT_DOTS_PER_PIXEL
	GAME_HEIGHT = 1080 * SQRT_DOTS_PER_PIXEL
	ZOOM_Y      = GAME_HEIGHT * ZOOM_X / GAME_WIDTH
	THRESHOLD   = float64(2)
)

// Wikipedia palette
var pre_palette [64]byte = [64]byte{
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

// TODO: maybe it could be better to use the % operator
func NewPalette() [][]byte {
	palette := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		palette[i] = make([]byte, MAX_STEP)
	}
	for i := 0; i < MAX_STEP; i++ {
		palette[0][i] = pre_palette[(i*4)%len(pre_palette)]
		palette[1][i] = pre_palette[(i*4+1)%len(pre_palette)]
		palette[2][i] = pre_palette[(i*4+2)%len(pre_palette)]
		palette[3][i] = pre_palette[(i*4+3)%len(pre_palette)]
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

type Game struct {
	stepDone int
	points   []complex128
	finished []bool
	buf      []byte
	palette  [][]byte
	image    *ebiten.Image
}

func NewGame() *Game {
	palette := NewPalette()
	g := &Game{
		stepDone: 0,
		points:   make([]complex128, GAME_WIDTH*GAME_HEIGHT),
		finished: make([]bool, GAME_WIDTH*GAME_HEIGHT),
		buf:      make([]byte, GAME_WIDTH*GAME_HEIGHT*4),
		image:    ebiten.NewImage(GAME_WIDTH, GAME_HEIGHT),
		palette:  palette,
	}
	g.ResetGame()
	return g
}

func (g *Game) ResetGame() {
	g.stepDone = 0
	for i := 0; i < GAME_WIDTH*GAME_HEIGHT; i++ {
		g.points[i] = STARTING_POINT
		g.finished[i] = false
		g.buf[i*4] = (g.palette)[0][0]
		g.buf[i*4+1] = (g.palette)[1][0]
		g.buf[i*4+2] = (g.palette)[2][0]
		g.buf[i*4+3] = (g.palette)[3][0]
	}
}

func (g *Game) Calculate(palette *[][]byte, stepsTodo int) {
	column := CENTRE + complex(-ZOOM_X/2, ZOOM_Y/2)
	stepX := complex(ZOOM_X/(GAME_WIDTH-1), 0)
	stepY := complex(0, -ZOOM_Y/(GAME_HEIGHT-1))

	ncpus := runtime.NumCPU()
	var wg sync.WaitGroup
	wg.Add(ncpus)
	di := GAME_WIDTH * ncpus
	stepYInner := complex(float64(ncpus), 0) * stepY
	for cpu := 0; cpu < ncpus; cpu, column = cpu+1, column+stepY {
		cpu := cpu
		c0 := column
		go func() {
			defer wg.Done()
			for i := cpu * GAME_WIDTH; i < GAME_WIDTH*GAME_HEIGHT; i, c0 = i+di, c0+stepYInner {
				c := c0
				stop := i + GAME_WIDTH
				for j := i; j < stop; j, c = j+1, c+stepX {
					// TODO: try to use channels, so I don't waste time with this if statement
					// Are those real performance improvements?
					if g.finished[j] {
						continue
					}
					z, deltaStep := DivSteps(c, g.points[j], stepsTodo)
					g.points[j] = z
					if deltaStep == -1 {
						continue
					}
					g.finished[j] = true
					steps := g.stepDone + deltaStep
					g.buf[j*4] = (*palette)[0][steps]
					g.buf[j*4+1] = (*palette)[1][steps]
					g.buf[j*4+2] = (*palette)[2][steps]
					g.buf[j*4+3] = (*palette)[3][steps]
				}
			}
		}()
	}
	wg.Wait()
	g.stepDone += stepsTodo

	g.image.WritePixels(g.buf)
}

func (g *Game) Update() error {
	if g.stepDone < MAX_STEP {
		start := time.Now()
		g.Calculate(&g.palette, min(STEP_PER_FRAME, MAX_STEP-g.stepDone))
		end := time.Now()
		log.Printf("Calculation took %dms (%.2f FPS?)",
			(end.Sub(start)).Milliseconds(), 1/(end.Sub(start)).Seconds())
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
