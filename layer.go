package slicer

import (
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/fogleman/fauxgl"
	"github.com/paulsmith/gogeos/geos"
)

type Layer struct {
	Z     float64
	Paths []Path
}

func (layer Layer) Buffer(x float64) Layer {
	g := pathsToGeos(layer.Paths)
	g = geos.Must(g.Buffer(x))
	p := geosToPaths(g)
	return Layer{layer.Z, p}
}

func SliceMesh(m *fauxgl.Mesh, step float64) []Layer {
	wn := runtime.NumCPU()
	minz := m.BoundingBox().Min.Z
	maxz := m.BoundingBox().Max.Z

	// copy triangles
	triangles := make([]*triangle, len(m.Triangles))
	var wg sync.WaitGroup
	for wi := 0; wi < wn; wi++ {
		wg.Add(1)
		go func(wi int) {
			for i := wi; i < len(m.Triangles); i += wn {
				triangles[i] = newTriangle(m.Triangles[i])
			}
			wg.Done()
		}(wi)
	}
	wg.Wait()

	// sort triangles
	sort.Slice(triangles, func(i, j int) bool {
		return triangles[i].MinZ < triangles[j].MinZ
	})

	// create jobs for workers
	n := int(math.Ceil((maxz-minz)/step)) + 1
	in := make(chan job, n)
	out := make(chan Layer, n)
	for wi := 0; wi < wn; wi++ {
		go worker(in, out)
	}
	index := 0
	var active []*triangle
	for i := 0; i < n; i++ {
		z := fauxgl.RoundPlaces(minz+step*float64(i), 8)
		// remove triangles below plane
		newActive := active[:0]
		for _, t := range active {
			if t.MaxZ >= z {
				newActive = append(newActive, t)
			}
		}
		active = newActive
		// add triangles above plane
		for index < len(triangles) && triangles[index].MinZ <= z {
			active = append(active, triangles[index])
			index++
		}
		// copy triangles for worker job
		activeCopy := make([]*triangle, len(active))
		copy(activeCopy, active)
		in <- job{z, activeCopy}
	}
	close(in)

	// read results from workers
	layers := make([]Layer, n)
	for i := 0; i < n; i++ {
		layers[i] = <-out
	}

	// sort layers
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].Z < layers[j].Z
	})
	return layers
}

type job struct {
	Z         float64
	Triangles []*triangle
}

func worker(in chan job, out chan Layer) {
	var paths []Path
	for j := range in {
		paths = paths[:0]
		p := plane{fauxgl.Vector{0, 0, j.Z}, fauxgl.Vector{0, 0, 1}}
		for _, t := range j.Triangles {
			if v1, v2, ok := p.intersectTriangle(t); ok {
				paths = append(paths, Path{v1, v2})
			}
		}
		out <- Layer{j.Z, joinPaths(paths)}
	}
}
