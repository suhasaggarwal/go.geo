package geo

import (
	"encoding/hex"
	"errors"
	"math"
)

var (
	// ErrUnsupportedDataType is returned by Scan methods when asked to scan
	// non []byte data from the database. This should never happen
	// if the driver is acting appropriately.
	ErrUnsupportedDataType = errors.New("go.geo: scan value must be []byte")

	// ErrNotWKB is returned when unmarshalling WKB and the data is not valid.
	ErrNotWKB = errors.New("go.geo: invalid WKB data")

	// ErrIncorrectGeometry is returned when unmarshalling WKB data into the wrong type.
	// For example, unmarshaling linestring data into a point.
	ErrIncorrectGeometry = errors.New("go.geo: incorrect geometry")
)

// NewPointFromWKB will take raw WKB and set the data for a new point.
// The WKB data must be of type Point. Will return nil if invalid WKB point.
func NewPointFromWKB(wkb []byte) *Point {
	p := &Point{}
	if err := p.unmarshalWKB(wkb); err != nil {
		return nil
	}

	return p
}

// NewLineFromWKB will take raw WKB and set the data for a new line.
// The WKB data must of type LineString and only contain 2 points.
// Will return nil if invalid WKB.
func NewLineFromWKB(wkb []byte) *Line {
	l := &Line{}
	if err := l.unmarshalWKB(wkb); err != nil {
		return nil
	}

	return l
}

// NewPointSetFromWKB will take raw WKB and set the data for a new point set.
// The WKB data must be of type LineString, Polygon or MultiPoint.
// Will return nil if invalid WKB.
func NewPointSetFromWKB(wkb []byte) *PointSet {
	ps := &PointSet{}
	if err := ps.unmarshalWKB(wkb); err != nil {
		return nil
	}

	return ps
}

// NewPathFromWKB will take raw WKB and set the data for a new path.
// The WKB data must be of type LineString, Polygon or MultiPoint.
// Will return nil if invalid WKB.
func NewPathFromWKB(wkb []byte) *Path {
	p := NewPath()
	if err := p.PointSet.unmarshalWKB(wkb); err != nil {
		return nil
	}

	return p
}

// Scan implements the sql.Scanner interface allowing
// point structs to be passed into rows.Scan(...interface{})
// The column must be of type Point and must be fetched in WKB format.
// Will attempt to parse MySQL's SRID+WKB format if the data is of the right size.
// If the column is empty (not null) an empty point (0, 0) will be returned.
func (p *Point) Scan(value interface{}) error {
	data, ok := value.([]byte)
	if !ok {
		return ErrUnsupportedDataType
	}

	//string of hex workaround
	//[]byte passed is a string of hex hence the need to decode hex string to real []byte
	if len(data) == 42 {
		dst := make([]byte, 21)
		_, err := hex.Decode(dst, data)
		if err != nil {
			return err
		}
	}

	if len(data) == 21 {
		// the length of a point type in WKB
		return p.unmarshalWKB(data)
	}

	if len(data) == 25 {
		// Most likely MySQL's SRID+WKB format.
		// However, could be a line string or multipoint with only one point.
		// But those would be invalid for parsing a point.
		return p.unmarshalWKB(data[4:])
	}

	if len(data) == 0 {
		// empty data, return empty go struct which in this case
		// would be [0,0]
		return nil
	}

	return ErrIncorrectGeometry
}

func (p *Point) unmarshalXY(data []byte, littleEndian bool) {
	p[0] = scanFloat64(data[:8], littleEndian)
	p[1] = scanFloat64(data[8:16], littleEndian)
}

func (p *Point) unmarshalWKB(data []byte) error {
	if len(data) != 21 {
		return ErrNotWKB
	}

	littleEndian, typeCode, err := scanPrefix(data)
	if err != nil {
		return err
	}

	if typeCode != 1 {
		return ErrIncorrectGeometry
	}

	p.unmarshalXY(data[5:], littleEndian)

	return nil
}

// Scan implements the sql.Scanner interface allowing
// line structs to be passed into rows.Scan(...interface{})
// The column must be of type LineString and contain 2 points,
// or an error will be returned. Data must be fetched in WKB format.
// Will attempt to parse MySQL's SRID+WKB format if the data is of the right size.
// If the column is empty (not null) an empty line [(0, 0), (0, 0)] will be returned.
func (l *Line) Scan(value interface{}) error {
	data, ok := value.([]byte)
	if !ok {
		return ErrUnsupportedDataType
	}

	if len(data) == 41 {
		// the length of a 2 point linestring type in WKB
		return l.unmarshalWKB(data)
	}

	if len(data) == 45 {
		// Most likely MySQL's SRID+WKB format.
		// However, could be some encoding of another type.
		// But those would be invalid for parsing a line.
		return l.unmarshalWKB(data[4:])
	}

	if len(data) == 0 {
		return nil
	}

	return ErrIncorrectGeometry
}

func (l *Line) unmarshalWKB(data []byte) error {
	if len(data) != 41 {
		return ErrNotWKB
	}

	littleEndian, typeCode, err := scanPrefix(data)
	if err != nil {
		return err
	}

	if typeCode != 2 {
		return ErrIncorrectGeometry
	}

	length := scanUint32(data[5:9], littleEndian)
	if length != 2 {
		return ErrIncorrectGeometry
	}

	l.a.unmarshalXY(data[9:], littleEndian)
	l.b.unmarshalXY(data[25:], littleEndian)

	return nil
}

// Scan implements the sql.Scanner interface allowing
// line structs to be passed into rows.Scan(...interface{})
// The column must be of type LineString, Polygon or MultiPoint
// or an error will be returned. Data must be fetched in WKB format.
// Will attempt to parse MySQL's SRID+WKB format if obviously no WKB
// or parsing as WKB fails.
// If the column is empty (not null) an empty point set will be returned.
func (ps *PointSet) Scan(value interface{}) error {
	data, ok := value.([]byte)
	if !ok {
		return ErrUnsupportedDataType
	}

	if len(data) == 0 {
		return nil
	}

	if len(data) < 6 {
		return ErrNotWKB
	}

	// first byte of real WKB data indicates endian and should 1 or 0.
	if data[0] == 0 || data[0] == 1 {
		return ps.unmarshalWKB(data)
	}

	return ps.unmarshalWKB(data[4:])
}

func (ps *PointSet) unmarshalLinearRing(data []byte, littleEndian bool) error {
	const pointSize = 16
	length := int(scanUint32(data[:4], littleEndian))
	if len(data) != 4+pointSize*length {
		return ErrNotWKB
	}
	points := make([]Point, length)
	for i := 0; i < length; i++ {
		points[i].unmarshalXY(data[4+pointSize*i:], littleEndian)
	}
	ps.SetPoints(points)
	return nil
}

func (ps *PointSet) unmarshalWKB(data []byte) error {
	if len(data) < 6 {
		return ErrNotWKB
	}

	littleEndian, typeCode, err := scanPrefix(data)
	if err != nil {
		return err
	}

	switch typeCode {
	case 2: // LineString
		return ps.unmarshalLinearRing(data[5:], littleEndian)
	case 3: // Polygon
		// For polygons there is a ring count.
		// We only allow one ring here.
		numRings := int(scanUint32(data[5:9], littleEndian))
		if numRings != 1 {
			return ErrIncorrectGeometry
		}
		return ps.unmarshalLinearRing(data[9:], littleEndian)
	case 4: // MultiPoint
		const wkbPointSize = 1 + 4 + 16
		length := int(scanUint32(data[5:9], littleEndian))
		if len(data) != 9+wkbPointSize*length {
			return ErrNotWKB
		}
		points := make([]Point, length)
		for i := 0; i < length; i++ {
			if err := points[i].unmarshalWKB(data[9+wkbPointSize*i : 9+wkbPointSize*i+wkbPointSize]); err != nil {
				return err
			}
		}
		ps.SetPoints(points)
	default:
		return ErrIncorrectGeometry
	}

	return nil
}

// Scan implements the sql.Scanner interface allowing
// line structs to be passed into rows.Scan(...interface{})
// The column must be of type LineString, Polygon or MultiPoint
// or an error will be returned. Data must be fetched in WKB format.
// Will attempt to parse MySQL's SRID+WKB format if obviously no WKB
// or parsing as WKB fails.
// If the column is empty (not null) an empty path will be returned.
func (p *Path) Scan(value interface{}) error {
	return p.PointSet.Scan(value)
}

func scanPrefix(data []byte) (bool, uint32, error) {
	if len(data) < 6 {
		return false, 0, ErrNotWKB
	}

	if data[0] == 0 {
		return false, scanUint32(data[1:5], false), nil
	}

	if data[0] == 1 {
		return true, scanUint32(data[1:5], true), nil
	}

	return false, 0, ErrNotWKB
}

func scanUint32(data []byte, littleEndian bool) uint32 {
	var v uint32

	if littleEndian {
		for i := 3; i >= 0; i-- {
			v <<= 8
			v |= uint32(data[i])
		}
	} else {
		for i := 0; i < 4; i++ {
			v <<= 8
			v |= uint32(data[i])
		}
	}

	return v
}

func scanFloat64(data []byte, littleEndian bool) float64 {
	var v uint64

	if littleEndian {
		for i := 7; i >= 0; i-- {
			v <<= 8
			v |= uint64(data[i])
		}
	} else {
		for i := 0; i < 8; i++ {
			v <<= 8
			v |= uint64(data[i])
		}
	}

	return math.Float64frombits(v)
}
