package workflow

import (
	"database/sql/driver"
	"fmt"

	domainworkflow "github.com/shinya/shineflow/domain/workflow"
	"github.com/shinya/shineflow/infrastructure/util"
)

// dslColumn 是 GORM 模型里 dsl 列的实际类型。
// 不直接给 domainworkflow.WorkflowDSL 加 Scan/Value，避免 domain 沾 database/sql。
type dslColumn domainworkflow.WorkflowDSL

func (d *dslColumn) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("dsl: unsupported scan type %T", src)
	}
	return util.UnmarshalFromString(s, (*domainworkflow.WorkflowDSL)(d))
}

func (d dslColumn) Value() (driver.Value, error) {
	return util.MarshalToString(domainworkflow.WorkflowDSL(d))
}
