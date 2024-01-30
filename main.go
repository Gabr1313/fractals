package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"log"
	"math"
	"runtime"
	"sync"
	"time"
	// "math/cmplx"
)

const (
	SCREEN_WIDTH        = 1080
	SCREEN_HEIGHT       = 1080
	SQRT_DOTS_PER_PIXEL = 2
	MAX_STEP            = 128
	CENTRE              = complex(-.5, 0)
	STARTING_POINT      = complex(0, 0)
	ZOOM_X              = float64(3)
	// should not be changed
	WIDTH               = 1080 * SQRT_DOTS_PER_PIXEL
	HEIGHT              = 1080 * SQRT_DOTS_PER_PIXEL
	ZOOM_Y              = HEIGHT * ZOOM_X / WIDTH
	RADIOUS             = float64(2)
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

	// DEFAULT
	/* for i := 0; i < SCREEN_HEIGHT*4; i += 4 {
		c := columnStart
		for j := 0; j < SCREEN_WIDTH*4; j += 4 {
			steps := DivSteps(c)
			g.buf[i*SCREEN_WIDTH+j] = (*palette)[0][steps]
			g.buf[i*SCREEN_WIDTH+j+1] = (*palette)[1][steps]
			g.buf[i*SCREEN_WIDTH+j+2] = (*palette)[2][steps]
			g.buf[i*SCREEN_WIDTH+j+3] = (*palette)[3][steps]
			c += stepX
		}
		columnStart += stepY
	}  */

	// BAD
	/* jobs := make(chan IdxPoint, SCREEN_HEIGHT*SCREEN_WIDTH)
	var wg sync.WaitGroup
	wg.Add(SCREEN_HEIGHT * SCREEN_WIDTH)
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for job := range jobs {
				steps := DivSteps(job.c)
				g.buf[job.idx] = (*palette)[0][steps]
				g.buf[job.idx+1] = (*palette)[1][steps]
				g.buf[job.idx+2] = (*palette)[2][steps]
				g.buf[job.idx+3] = (*palette)[3][steps]
				wg.Done()
			}
		}()
	}
	for i := 0; i < SCREEN_HEIGHT*4; i += 4 {
		c := columnStart
		for j := 0; j < SCREEN_WIDTH*4; j += 4 {
			jobs <- IdxPoint{i*SCREEN_WIDTH + j, c}
			c += stepX
		}
		columnStart += stepY
	}
	close(jobs)  */

	// BETTER
	/* var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	iJump := SCREEN_HEIGHT * 4 / runtime.NumCPU()
	for i := 0; i < SCREEN_HEIGHT*4; i += iJump {
		i := i
		columnStart := columnStart + stepY*complex(float64(i/4), 0)
		go func() {
			defer wg.Done()
			// time1 := time.Duration(0)
			// time2 := time.Duration(0)
			stop:= min(i+iJump, SCREEN_HEIGHT*4)
			for ; i < stop; i += 4 {
				c := columnStart
				for j := 0; j < SCREEN_WIDTH*4; j += 4 {
					// tt := time.Now()
					steps := DivSteps(c)
					// time1 += time.Now().Sub(tt)
					// tt = time.Now()
					g.buf[i*SCREEN_WIDTH+j] = (*palette)[0][steps]
					g.buf[i*SCREEN_WIDTH+j+1] = (*palette)[1][steps]
					g.buf[i*SCREEN_WIDTH+j+2] = (*palette)[2][steps]
					g.buf[i*SCREEN_WIDTH+j+3] = (*palette)[3][steps]
					// time2 += time.Now().Sub(tt)
					c += stepX
				}
				columnStart += stepY
			}
			// log.Printf("Time 1: %v | Time 2: %v", time1, time2)
		}()
	}
	wg.Wait()   */

	// BEST
	jobs := make(chan IdxPoint, HEIGHT)
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for job := range jobs {
				idx := job.idx
				c := job.c
				for j := 0; j < WIDTH*4; j += 4 {
					steps := DivSteps(c)
					g.buf[idx] = (*palette)[0][steps]
					g.buf[idx+1] = (*palette)[1][steps]
					g.buf[idx+2] = (*palette)[2][steps]
					g.buf[idx+3] = (*palette)[3][steps]
					idx += 4
					c += stepX
				}
			}
			wg.Done()
		}()
	}
	for i := 0; i < WIDTH*HEIGHT*4; i += 4 * WIDTH {
		c := columnStart
		jobs <- IdxPoint{i, c}
		columnStart += stepY
	}
	close(jobs)
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
