package tuning

import (
	"testing"
)

func TestTraversableImageRender_NoImage(t *testing.T) {
	text, err := TraversableImageRender("hey", 5, 10, 10, 1)
	if err == nil {
		t.Fatalf("Expected to not have a valid image in path 'hey'. Got:\n%s", text)
	}

	t.Logf("Error: %s", err)
}

func TestTraversableImageRender_Image(t *testing.T) {
	text, err := TraversableImageRender("../../data/map.png", 61, 114, 10, 1)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(text)
}
