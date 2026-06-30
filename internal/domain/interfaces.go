package domain

// GridData is the minimal rectangular cost-grid interface consumed by rasterizers.
type GridData interface {
	Dimensions() (rows, cols uint)
	CellData() []int32
}

////////////

// INode is the emergency signaling node shape consumed by flow-field routing.
type INode interface {
	Id() string
	Position() (x, y, z float64, floor int)
	//SetDirection(d Direction)
	//Direction() Direction
}
