package convergence

// overlayPtr returns a clone of overlay when set, otherwise a clone of base. It
// implements field-level precedence where the workspace value wins over global.
func overlayPtr[T any](base, overlay *T) *T {
	if overlay != nil {
		cloned := *overlay
		return &cloned
	}
	if base != nil {
		cloned := *base
		return &cloned
	}
	return nil
}

// overlaySlicePtr returns a clone of overlay when set, otherwise a clone of base.
func overlaySlicePtr(base, overlay *[]string) *[]string {
	source := overlay
	if source == nil {
		source = base
	}
	if source == nil {
		return nil
	}
	cloned := append([]string(nil), *source...)
	return &cloned
}
