package workflow

// NodeUI 仅供画布使用，不影响执行。Width / Height 为 nil 时使用 NodeType 默认尺寸。
type NodeUI struct {
	X      float64
	Y      float64
	Width  *float64
	Height *float64
}
