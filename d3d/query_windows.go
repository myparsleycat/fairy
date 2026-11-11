package d3d

import (
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

// D3D11_QUERY values and the GetData flag.
const (
	QUERY_TIMESTAMP          = 2
	QUERY_TIMESTAMP_DISJOINT = 3
	ASYNC_GETDATA_DONOTFLUSH = 1
)

// Query wraps ID3D11Query.
type Query struct{ P unsafe.Pointer }

// QueryDesc is D3D11_QUERY_DESC.
type QueryDesc struct {
	Query, MiscFlags uint32
}

// QueryDataTimestampDisjoint is D3D11_QUERY_DATA_TIMESTAMP_DISJOINT, the
// result of a QUERY_TIMESTAMP_DISJOINT query: the timestamp frequency in Hz,
// and whether timestamps taken during the query are unreliable.
type QueryDataTimestampDisjoint struct {
	Frequency uint64
	Disjoint  int32
	_         uint32
}

func (q Query) Release() { com.Release(q.P) }

func (d Device) CreateQuery(desc *QueryDesc) (Query, error) {
	var q unsafe.Pointer
	err := com.HR(d.P, 24, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&q))).Err("CreateQuery")
	return Query{q}, err
}

func (c Context) Begin(q Query) { com.Call(c.P, 27, uintptr(q.P)) }
func (c Context) End(q Query)   { com.Call(c.P, 28, uintptr(q.P)) }

// GetData returns false without an error while a query is still pending.
// data must point to size bytes in the layout required by the query type.
func (c Context) GetData(q Query, data unsafe.Pointer, size, flags uint32) (bool, error) {
	hr := com.HR(c.P, 29, uintptr(q.P), uintptr(data), uintptr(size), uintptr(flags))
	return hr == 0, hr.Err("GetData")
}
