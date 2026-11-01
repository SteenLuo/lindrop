package main

// All coordinates are logical pixels. Painting and native controls share them.
type composerGeometry struct {
	Top, ListHeight, TextTop, TextHeight, PreviewTop int
}

func composerLayout(height, requested, pictures int) composerGeometry {
	preview := 0
	if pictures > 0 {
		preview = 76
	}
	if requested == 0 {
		requested = 96
	}
	maximum := height - 74 - 160 - 89 - preview
	if maximum < 64 {
		maximum = 64
	}
	if requested < 64 {
		requested = 64
	}
	if requested > maximum {
		requested = maximum
	}
	top := height - 89 - requested - preview
	return composerGeometry{top, top - 74, top + 44 + preview, requested, top + 41}
}
