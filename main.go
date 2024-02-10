package main

// @todo  refactor: logically separate screen 0 and screen 1 removing many if
//        statementes
// @todo? ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

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
	STARTING_POINT_0    = complex(0, 0)
	STARTING_POINT_1    = complex(0, 0)

	// INITIAL_CENTER_0 = complex(0, 1)
	// INITIAL_ZOOM   = 2.5e-13
	INITIAL_CENTER_0  = complex(-.75, 0)
	INITIAL_CENTER_1  = complex(0, 0)
	INITIAL_ZOOM      = 2.5e-0
	DELTA_STEP        = 1024
	MAX_STEP          = DELTA_STEP * 8
	ZOOM_DELTA        = .94
	MOUSE_WHEEL_SPEED = 1
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
	idx0         chan int
	idx1         chan int
	canReload0   chan bool
	canReload1   chan bool
	haveToStop   chan bool
	canStart     chan bool
	workingCount sync.WaitGroup
}

type DoublePoints struct {
	zoomX0    float64
	zoomY0    float64
	zoomX1    float64
	zoomY1    float64
	curr0     int
	curr1     int
	dbCentre0 [2]complex128
	dbCentre1 [2]complex128
	z0        complex128
	c1        complex128
	dbPoints  [2][]PointStatus
}

type Game struct {
	points        DoublePoints
	movementSpeed int
	th            ThreadStatus
	keys          []ebiten.Key
	mouse         Mouse
	buf           Buffer
	currentScreen int
	image         *ebiten.Image
}

func main() {
	ebiten.SetWindowSize(DB_SCREEN_WIDTH, SCREEN_HEIGHT)
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
	return DB_GAME_WIDTH, GAME_HEIGHT
}

func NewGame() *Game {
	nThreads := runtime.NumCPU()
	g := Game{
		points: DoublePoints{
			zoomX0:    INITIAL_ZOOM,
			zoomY0:    INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
			zoomX1:    INITIAL_ZOOM,
			zoomY1:    INITIAL_ZOOM * GAME_HEIGHT / GAME_WIDTH,
			z0:        STARTING_POINT_0,
			c1:        STARTING_POINT_1,
			curr0:     0,
			curr1:     0,
			dbCentre0: [2]complex128{INITIAL_CENTER_0, INITIAL_CENTER_0},
			dbCentre1: [2]complex128{INITIAL_CENTER_1, INITIAL_CENTER_1},
			dbPoints: [2][]PointStatus{
				make([]PointStatus, DB_GAME_WIDTH*GAME_HEIGHT),
				make([]PointStatus, DB_GAME_WIDTH*GAME_HEIGHT),
			},
		},
		movementSpeed: MOVEMENT_SPEED,
		th: ThreadStatus{
			nThreads:     nThreads,
			idx0:         make(chan int),
			idx1:         make(chan int),
			haveToStop:   make(chan bool, nThreads),
			canStart:     make(chan bool, nThreads),
			canReload0:   make(chan bool, 1),
			canReload1:   make(chan bool, 1),
			workingCount: sync.WaitGroup{},
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
	g.th.canReload1 <- true
	g.currentScreen = 1
	g.Reload(g.currentScreen)
	g.th.canReload0 <- true
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
			float64(x-(GAME_WIDTH)/2-DisplacementX(g.currentScreen))*g.points.zoomX0/GAME_WIDTH,
			float64(-y+GAME_HEIGHT/2)*g.points.zoomY0/GAME_HEIGHT,
		)
		if g.currentScreen == 0 {
			mousePoint += g.points.dbCentre0[g.points.curr0]
			if mousePoint != g.points.c1 {
				g.points.c1 = mousePoint
				g.Reload(1 - g.currentScreen)
			}
		} else {
			mousePoint += g.points.dbCentre1[g.points.curr1]
			if mousePoint != g.points.z0 {
				g.points.z0 = mousePoint
				g.Reload(1 - g.currentScreen)
			}
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
	for _, k := range g.keys {
		switch k {
		case ebiten.KeyE:
			if g.currentScreen == 0 {
				g.points.z0 = STARTING_POINT_0
				g.points.dbCentre0[g.points.curr0] = INITIAL_CENTER_0
				g.points.zoomX0 = INITIAL_ZOOM
				g.points.zoomY0 = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			} else {
				g.points.c1 = STARTING_POINT_1
				g.points.dbCentre1[g.points.curr1] = INITIAL_CENTER_1
				g.points.zoomX1 = INITIAL_ZOOM
				g.points.zoomY1 = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			}
			g.Reload(g.currentScreen)
		case ebiten.KeyR:
			if g.currentScreen == 0 {
				g.points.dbCentre0[g.points.curr0] = INITIAL_CENTER_0
				g.points.zoomX0 = INITIAL_ZOOM
				g.points.zoomY0 = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			} else {
				g.points.dbCentre1[g.points.curr1] = INITIAL_CENTER_1
				g.points.zoomX1 = INITIAL_ZOOM
				g.points.zoomY1 = GAME_HEIGHT * INITIAL_ZOOM / GAME_WIDTH
			}
			g.Reload(g.currentScreen)
		case ebiten.KeyQ:
			return ebiten.Termination
		case ebiten.KeyH, ebiten.KeyArrowLeft:
			g.PartiallyReload(g.currentScreen, g.movementSpeed, 0)
		case ebiten.KeyJ, ebiten.KeyArrowDown:
			g.PartiallyReload(g.currentScreen, 0, -g.movementSpeed)
		case ebiten.KeyK, ebiten.KeyArrowUp:
			g.PartiallyReload(g.currentScreen, 0, g.movementSpeed)
		case ebiten.KeyL, ebiten.KeyArrowRight:
			g.PartiallyReload(g.currentScreen, -g.movementSpeed, 0)
		case ebiten.KeyF, ebiten.KeySpace:
			g.Zoom(ZOOM_DELTA)
		case ebiten.KeyD, ebiten.KeyBackspace:
			g.Zoom(1 / ZOOM_DELTA)
		}
	}
	return nil
}

func (g *Game) Zoom(zoom float64) {
	if g.currentScreen == 0 {
		newZoom := g.points.zoomX0 * zoom
		if newZoom < MIN_ZOOM {
			return
		}
		g.points.zoomX0 = newZoom
		g.points.zoomY0 = newZoom * GAME_HEIGHT / GAME_WIDTH
	} else {
		newZoom := g.points.zoomX1 * zoom
		if newZoom < MIN_ZOOM {
			return
		}
		g.points.zoomX1 = newZoom
		g.points.zoomY1 = newZoom * GAME_HEIGHT / GAME_WIDTH
	}
	g.Reload(g.currentScreen)
}

func (g *Game) ZoomFixedMouse(zoom float64, mouseX, mouseY int) {
	displacement := DisplacementX(g.currentScreen)
	if g.currentScreen == 0 {
		newZoom := g.points.zoomX0 * zoom
		if newZoom < MIN_ZOOM {
			return
		}
		fixedPoint := complex(
			float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points.zoomX0/GAME_WIDTH,
			float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY0/GAME_HEIGHT,
		)
		g.points.zoomX0 = newZoom
		g.points.zoomY0 = newZoom * GAME_HEIGHT / GAME_WIDTH
		delta := fixedPoint - complex(
			float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points.zoomX0/GAME_WIDTH,
			float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY0/GAME_HEIGHT,
		)
		g.points.dbCentre0[g.points.curr0] += delta
	} else {
		newZoom := g.points.zoomX1 * zoom
		if newZoom < MIN_ZOOM {
			return
		}
		fixedPoint := complex(
			float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points.zoomX1/GAME_WIDTH,
			float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY1/GAME_HEIGHT,
		)
		g.points.zoomX1 = newZoom
		g.points.zoomY1 = newZoom * GAME_HEIGHT / GAME_WIDTH
		delta := fixedPoint - complex(
			float64(g.mouse.x-(GAME_WIDTH)/2-displacement)*g.points.zoomX1/GAME_WIDTH,
			float64(-g.mouse.y+GAME_HEIGHT/2)*g.points.zoomY1/GAME_HEIGHT,
		)
		g.points.dbCentre1[g.points.curr1] += delta
	}
	g.Reload(g.currentScreen)
}

func (g *Game) Reload(screen int) {
	g.DoTheMath(screen, 42, 42, false)
}

func (g *Game) PartiallyReload(screen int, dx, dy int) {
	g.DoTheMath(screen, dx, dy, true)
}

func (g *Game) DoTheMath(screen int, dx, dy int, cpyFlag bool) {
	if screen == 0 {
		select {
		case <-g.th.canReload0:
			defer func() {
				g.th.canReload0 <- true
			}()
		default:
			return
		}
	} else {
		select {
		case <-g.th.canReload1:
			defer func() {
				g.th.canReload1 <- true
			}()
		default:
			return
		}
	}
	for cpu := 0; cpu < g.th.nThreads; cpu++ {
		g.th.haveToStop <- true
	}
	g.th.workingCount.Wait()
	defer g.th.workingCount.Add(g.th.nThreads)
	if screen == 0 {
		close(g.th.idx0)
		g.th.idx0 = make(chan int, GAME_WIDTH*GAME_HEIGHT)
	} else {
		close(g.th.idx1)
		g.th.idx1 = make(chan int, GAME_WIDTH*GAME_HEIGHT)
	}

	var leftUp complex128
	var deltaX float64
	var deltaY float64
	if screen == 0 {
		if cpyFlag {
			g.points.curr0 = 1 - g.points.curr0
			g.points.dbCentre0[g.points.curr0] = g.points.dbCentre0[1-g.points.curr0] +
				complex(float64(-dx)*g.points.zoomX0/GAME_WIDTH,
					float64(dy)*g.points.zoomY0/GAME_HEIGHT)
		}
		leftUp = g.points.dbCentre0[g.points.curr0] + complex(-g.points.zoomX0/2, g.points.zoomY0/2)
		deltaX = g.points.zoomX0 / (GAME_WIDTH - 1)
		deltaY = -g.points.zoomY0 / (GAME_HEIGHT - 1)
	} else {
		if cpyFlag {
			g.points.curr1 = 1 - g.points.curr1
			g.points.dbCentre1[g.points.curr1] = g.points.dbCentre1[1-g.points.curr1] +
				complex(float64(-dx)*g.points.zoomX1/GAME_WIDTH,
					float64(dy)*g.points.zoomY1/GAME_HEIGHT)
		}
		leftUp = g.points.dbCentre1[g.points.curr1] + complex(-g.points.zoomX1/2, g.points.zoomY1/2)
		deltaX = g.points.zoomX1 / (GAME_WIDTH - 1)
		deltaY = -g.points.zoomY1 / (GAME_HEIGHT - 1)
	}

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

	displacement := DisplacementX(screen)
	for cpu := 0; cpu < g.th.nThreads; cpu = cpu + 1 {
		minI := cpu
		go func() {
			for i := minI; i < GAME_HEIGHT; i = i + g.th.nThreads {
				for j := 0; j < GAME_WIDTH; j++ {
					// maybe not anymore... @todo
					// it could be doable without the if, if tha game could reset
					// to the previous frame but I don't think it would be enjoiable
					// to not see anything drawn while dragging the mouse
					// maybe it is better a little lag
					// if !cpyFlag {
					// 	select {
					// 	case <-g.th.haveToStop:
					// 		g.th.workingCount.Done()
					// 		return
					// 	default:
					// 	}
					// }
					if screen == 0 {
						idx := i*DB_GAME_WIDTH + j + displacement
						point := &g.points.dbPoints[g.points.curr0][idx]
						if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
							prevIdx := (i-dy)*DB_GAME_WIDTH + (j - dx) + displacement
							prevPoint := &g.points.dbPoints[1-g.points.curr0][prevIdx]
							*point = *prevPoint
						} else {
							point.c = leftUp + complex(float64(j)*deltaX, float64(i)*deltaY)
							point.z = g.points.z0
							point.steps = 0
							point.finished = false
						}
						g.WriteToBuffer(idx, point.steps)
						if !point.finished {
							g.th.idx0 <- idx
						}
					} else {
						idx := i*DB_GAME_WIDTH + j + displacement
						point := &g.points.dbPoints[g.points.curr1][idx]
						if (yStart <= i && i < yEnd) && (xStart <= j && j < xEnd) {
							prevIdx := (i-dy)*DB_GAME_WIDTH + (j - dx) + displacement
							prevPoint := &g.points.dbPoints[1-g.points.curr1][prevIdx]
							*point = *prevPoint
						} else {
							point.c = g.points.c1
							point.z = leftUp + complex(float64(j)*deltaX, float64(i)*deltaY)
							point.steps = 0
							point.finished = false
						}
						g.WriteToBuffer(idx, point.steps)
						if !point.finished {
							g.th.idx1 <- idx
						}
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
		case idx := <-g.th.idx0:
			point := &g.points.dbPoints[g.points.curr0][idx]
			z, stepDone := EscapeSteps(point.z, point.c, DELTA_STEP)
			point.z = z
			if stepDone == -1 {
				point.steps += DELTA_STEP
				if point.steps < MAX_STEP {
					g.th.idx0 <- idx
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
		case idx := <-g.th.idx1:
			point := &g.points.dbPoints[g.points.curr1][idx]
			z, stepDone := EscapeSteps(point.z, point.c, DELTA_STEP)
			point.z = z
			if stepDone == -1 {
				point.steps += DELTA_STEP
				if point.steps < MAX_STEP {
					g.th.idx1 <- idx
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

func DisplacementX(screenNumber int) int {
	if screenNumber != 0 {
		return GAME_WIDTH
	}
	return 0
}
