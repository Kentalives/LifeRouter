package mock

import (
	"context"
	"math"
	"testing"
)

func TestGeoTruth_NearbyObjectsOf(t *testing.T) {
	ex, err := NewExternalSystem([]string{"first", "second"}, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := NewGeoTruth(5, ex)

	source := &Element{IdName: "source", X: 0, Y: 0, Z: 1, Width: 2, Height: 4}
	near := &Element{IdName: "near", X: 3, Y: 4, Z: 1, Width: 2, Height: 6}
	atRadius := &Element{IdName: "at-radius", X: 5, Y: 0, Z: 1, RotY: math.Pi / 2, Width: 2, Height: 4}
	far := &Element{IdName: "far", X: 6, Y: 0, Z: 1, Width: 2, Height: 4}
	otherFloor := &Element{IdName: "other-floor", X: 1, Y: 1, Z: 10, Width: 2, Height: 4}

	for _, elem := range []*Element{source, near, atRadius, far, otherFloor} {
		if err := geoTruth.AddObject(elem); err != nil {
			t.Fatal(err)
		}
	}

	got, err := geoTruth.NearbyObjectsOf(context.Background(), source.Id(), 5, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("expected two nearby objects, got %d: %#v", len(got), got)
	}

	byID := make(map[string]bool, len(got))
	for _, obj := range got {
		if obj.ID == source.Id() {
			t.Fatal("source object should not see itself")
		}
		byID[obj.ID] = true
	}

	if !byID[near.Id()] {
		t.Fatalf("expected nearby object %q", near.Id())
	}
	if !byID[atRadius.Id()] {
		t.Fatalf("expected object exactly at radius %q", atRadius.Id())
	}
	if byID[far.Id()] {
		t.Fatalf("did not expect object outside radius %q", far.Id())
	}
	if byID[otherFloor.Id()] {
		t.Fatalf("did not expect object on a different floor %q", otherFloor.Id())
	}
}

func TestGeoTruth_NearbyObjectsOfReturnsOrientedBounds(t *testing.T) {
	ex, err := NewExternalSystem([]string{""}, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := NewGeoTruth(2, ex)

	source := &Element{IdName: "source", X: 0, Y: 0, Z: 0, Width: 1, Height: 1}
	near := &Element{IdName: "near", X: 10, Y: 20, Z: 0, Width: 4, Height: 2}

	for _, elem := range []*Element{source, near} {
		if err := geoTruth.AddObject(elem); err != nil {
			t.Fatal(err)
		}
	}

	got, err := geoTruth.NearbyObjectsOf(context.Background(), source.Id(), 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one nearby object, got %d", len(got))
	}

	want := near.ToGeoTruthOrientedObject()
	if got[0].ID != want.ID || got[0].Position != want.Position || got[0].Bounds != want.Bounds {
		t.Fatalf("nearby object did not match oriented object conversion\nwant: %#v\n got: %#v", want, got[0])
	}
}

func TestGeoTruth_NearbyObjectsOfMissingSource(t *testing.T) {
	ex, err := NewExternalSystem([]string{""}, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := NewGeoTruth(0, ex)

	if _, err := geoTruth.NearbyObjectsOf(context.Background(), "missing", 5, nil); err == nil {
		t.Fatal("expected error for missing source object")
	}
}
