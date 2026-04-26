package run

import (
	"database/sql/driver"
	"fmt"

	domainrun "github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/infrastructure/util"
)

// runErrorColumn 是 workflow_runs.error 列的 Scanner/Valuer。
type runErrorColumn struct{ inner *domainrun.RunError }

func (c *runErrorColumn) Scan(src any) error {
	if src == nil {
		c.inner = nil
		return nil
	}
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("run error: unsupported scan type %T", src)
	}
	var e domainrun.RunError
	if err := util.UnmarshalFromString(s, &e); err != nil {
		return err
	}
	c.inner = &e
	return nil
}

func (c runErrorColumn) Value() (driver.Value, error) {
	if c.inner == nil {
		return nil, nil
	}
	return util.MarshalToString(*c.inner)
}

// nodeErrorColumn 是 workflow_node_runs.error 列。
type nodeErrorColumn struct{ inner *domainrun.NodeError }

func (c *nodeErrorColumn) Scan(src any) error {
	if src == nil {
		c.inner = nil
		return nil
	}
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("node error: unsupported scan type %T", src)
	}
	var e domainrun.NodeError
	if err := util.UnmarshalFromString(s, &e); err != nil {
		return err
	}
	c.inner = &e
	return nil
}

func (c nodeErrorColumn) Value() (driver.Value, error) {
	if c.inner == nil {
		return nil, nil
	}
	return util.MarshalToString(*c.inner)
}

// externalRefsColumn 是 workflow_node_runs.external_refs 列。
type externalRefsColumn []domainrun.ExternalRef

func (c *externalRefsColumn) Scan(src any) error {
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
		return fmt.Errorf("external_refs: unsupported scan type %T", src)
	}
	return util.UnmarshalFromString(s, (*[]domainrun.ExternalRef)(c))
}

func (c externalRefsColumn) Value() (driver.Value, error) {
	return util.MarshalToString([]domainrun.ExternalRef(c))
}
