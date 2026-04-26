package plugin

import (
	"database/sql/driver"
	"fmt"

	domainworkflow "github.com/shinya/shineflow/domain/workflow"
	"github.com/shinya/shineflow/infrastructure/util"
)

// stringMapColumn 复用给 headers / query_params / response_mapping 三列。
type stringMapColumn map[string]string

func (c *stringMapColumn) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	case nil:
		*c = nil
		return nil
	default:
		return fmt.Errorf("string map: unsupported scan type %T", src)
	}
	return util.UnmarshalFromString(s, (*map[string]string)(c))
}

func (c stringMapColumn) Value() (driver.Value, error) {
	if c == nil {
		return "{}", nil
	}
	return util.MarshalToString(map[string]string(c))
}

// portSpecsColumn 给 input_schema / output_schema 两列。
type portSpecsColumn []domainworkflow.PortSpec

func (c *portSpecsColumn) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	case nil:
		*c = nil
		return nil
	default:
		return fmt.Errorf("port specs: unsupported scan type %T", src)
	}
	return util.UnmarshalFromString(s, (*[]domainworkflow.PortSpec)(c))
}

func (c portSpecsColumn) Value() (driver.Value, error) {
	if c == nil {
		return "[]", nil
	}
	return util.MarshalToString([]domainworkflow.PortSpec(c))
}
