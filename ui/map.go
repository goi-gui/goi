package ui

import (
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/tree"
)

// The tile scheduler. OpenStreetMap's tile policy asks for a light, identified
// traffic profile, so one app keeps two downloads in flight and 256 decoded
// tiles in memory; a larger tile that a design points at is still just a tile.
const (
	mapTileParallel   = 2
	mapTileCacheMax   = 128
	mapTileCacheBytes = 32 << 20
	// mapQueueMax bounds the queue when the user zooms through many levels
	// quickly. A dropped URL is not lost: the next frame asks again.
	mapQueueMax = 64
)

// syncMaps prepares every visible Map and hands it the tiles it needs. It runs
// after layout, because the viewport that fits a bbox is the laid-out box.
func (a *App) syncMaps() {
	if !a.hasMaps || a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.WalkVisible(func(n tree.Node) {
		m, ok := n.(*tree.Map)
		if !ok {
			return
		}
		b := m.Bounds()
		m.Prepare(b.W, b.H)
		m.ResetTiles()
		for _, t := range m.ViewTiles() {
			url := tileURL(m.Tiles.Get(), t.MapTile)
			if d, ok := a.images[url]; ok && d != nil {
				m.SetTile(t.MapTile, d)
				continue
			}
			a.wantTile(url)
		}
	})
	a.pumpTiles()
}

// wantTile queues a tile URL once. A failed URL stays failed: the tile really is
// missing, and retrying it every frame would only hammer the server.
func (a *App) wantTile(url string) {
	if a.mapFailed[url] || a.mapPending[url] || len(a.mapQueue) >= mapQueueMax {
		return
	}
	if a.mapPending == nil {
		a.mapPending = map[string]bool{}
	}
	a.mapPending[url] = true
	a.mapQueue = append(a.mapQueue, url)
}

// pumpTiles starts downloads until the concurrency budget is full.
func (a *App) pumpTiles() {
	for a.mapInFlight < mapTileParallel && len(a.mapQueue) > 0 {
		url := a.mapQueue[0]
		a.mapQueue = a.mapQueue[1:]
		a.mapInFlight++
		go func() {
			d, _ := fetchTile(url)
			// Post runs on the UI goroutine, the only place a node may change.
			a.Post(func() { a.finishTile(url, d) })
		}()
	}
}

// fetchTile downloads and decodes one tile off the UI goroutine.
func fetchTile(url string) (*img.Data, error) {
	b, err := fetchRemote(url)
	if err != nil {
		return nil, err
	}
	return img.Decode(b)
}

// finishTile caches a landed tile, repaints the maps and starts the next one.
func (a *App) finishTile(url string, d *img.Data) {
	a.mapInFlight--
	delete(a.mapPending, url)
	if d == nil {
		if a.mapFailed == nil {
			a.mapFailed = map[string]bool{}
		}
		a.mapFailed[url] = true
	} else {
		a.cacheTile(url, d)
		a.evictTiles()
	}
	if a.win != nil && a.win.tree != nil {
		a.win.tree.Walk(func(n tree.Node) {
			if m, ok := n.(*tree.Map); ok {
				m.Base().MarkDirty(tree.DirtyPaint)
			}
		})
	}
	a.pumpTiles()
}

// evictTiles drops the oldest decoded tiles once the cache is over its cap. A
// Map keeps its own pointer to a tile it is showing, so an evicted bitmap is
// still drawn this frame and simply fetched again if the view returns to it.
func (a *App) evictTiles() {
	for len(a.mapKeys) > 0 && (len(a.mapKeys) > mapTileCacheMax || a.mapBytes > mapTileCacheBytes) {
		url := a.mapKeys[0]
		a.mapKeys = a.mapKeys[1:]
		if d, ok := a.images[url]; ok {
			a.mapBytes -= len(d.Pix)
			delete(a.images, url)
		}
		delete(a.anims, url)
	}
}

// tileURL fills the `{z}/{x}/{y}` template, plus `{s}` with one of the three
// subdomain letters older tile services use. It runs for every visible tile on
// every frame, so it builds the string in one pass.
func tileURL(template string, t tree.MapTile) string {
	if template == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(template) + 12)
	for i := 0; i < len(template); {
		if template[i] == '{' {
			if j := strings.IndexByte(template[i:], '}'); j > 0 {
				switch template[i+1 : i+j] {
				case "z":
					b.WriteString(strconv.Itoa(t.Z))
				case "x":
					b.WriteString(strconv.Itoa(t.X))
				case "y":
					b.WriteString(strconv.Itoa(t.Y))
				case "s":
					b.WriteByte("abc"[(t.X+t.Y)%3])
				default:
					b.WriteString(template[i : i+j+1])
				}
				i += j + 1
				continue
			}
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String()
}
