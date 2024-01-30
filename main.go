package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"log"
	"math"
	"sync"
	"time"
	// "math/cmplx"
)

const (
	SCREEN_WIDTH        = 1080
	SCREEN_HEIGHT       = 1080
	SQRT_DOTS_PER_PIXEL = 1
	MAX_STEP            = 128
	CENTRE              = complex(-.5, 0)
	STARTING_POINT      = complex(0, 0)
	ZOOM_X              = float64(3)
	// below value should not be changed
	WIDTH   = 1080 * SQRT_DOTS_PER_PIXEL
	HEIGHT  = 1080 * SQRT_DOTS_PER_PIXEL
	ZOOM_Y  = HEIGHT * ZOOM_X / WIDTH
	RADIOUS = float64(2)
)

func NewPalette() [][]byte {
	palette := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		palette[i] = make([]byte, MAX_STEP+1)
	}
	maxColor := float64(0xff)
	for i := 0; i <= MAX_STEP; i++ {
		palette[0][i] = byte(math.Sqrt(float64(i)/float64(MAX_STEP)) * maxColor)
		palette[1][i] = byte(math.Sqrt(float64(i)/float64(MAX_STEP)) * maxColor)
		palette[2][i] = byte(math.Sqrt(float64(i)/float64(MAX_STEP)) * maxColor)
		palette[3][i] = 0xff
	}
	return palette
}

func DivSteps(c complex128) int {
	z := STARTING_POINT
	for i := 0; i < MAX_STEP; i++ {
		// MANDELBROT SET
		z = z*z + c
		if real(z)*real(z)+imag(z)*imag(z) > RADIOUS*RADIOUS {
			return i
		}

		// JULIA SET: problem with RADIOUS (how to know when a point diverges?)
		// c = c*c + z
		// if real(c)*real(c)+imag(c)*imag(c) > RADIUS*RADIUS {
		// 	return i
		// }
	}
	return MAX_STEP
	// return 0
}

type Game struct {
	buf   []byte
	image *ebiten.Image
}

type IdxStep struct {
	idx, step int
}

type IdxPoint struct {
	idx int
	c   complex128
}

func NewGame() *Game {
	palette := NewPalette()
	g := &Game{
		buf:   make([]byte, WIDTH*HEIGHT*4),
		image: ebiten.NewImage(WIDTH, HEIGHT),
	}
	start := time.Now()
	g.Calculate(&palette)
	end := time.Now()
	log.Printf("Calculation took %dms-> %.2f FPS",
		(end.Sub(start)).Milliseconds(), 1/(end.Sub(start)).Seconds())
	return g
}

func (g *Game) Calculate(palette *[][]byte) {
	columnStart := CENTRE + complex(-ZOOM_X/2, ZOOM_Y/2)
	stepX := complex(ZOOM_X/(WIDTH-1), 0)
	stepY := complex(0, -ZOOM_Y/(HEIGHT-1))

	var wg sync.WaitGroup
	wg.Add(HEIGHT)
	for i := 0; i < WIDTH*HEIGHT*4; i += 4 * WIDTH {
		c := columnStart
		j := i
		go func() {
			stop := j + WIDTH*4
			for ; j < stop; j += 4 {
				steps := DivSteps(c)
				g.buf[j] = (*palette)[0][steps]
				g.buf[j+1] = (*palette)[1][steps]
				g.buf[j+2] = (*palette)[2][steps]
				g.buf[j+3] = (*palette)[3][steps]
				c += stepX
			}
			wg.Done()
		}()
		columnStart += stepY
	}
	wg.Wait() 

	g.image.WritePixels(g.buf)
}

func (g *Game) Update() error {
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.image, nil)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return WIDTH, HEIGHT
}

func main() {
	ebiten.SetWindowSize(SCREEN_WIDTH, SCREEN_HEIGHT)
	ebiten.SetWindowTitle("Mandelbrot set")
	if err := ebiten.RunGame(NewGame()); err != nil {
		log.Fatal(err)
	}
}
