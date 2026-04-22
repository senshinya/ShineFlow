// Package domain 是领域层。
//
// 职责：核心业务规则 —— 实体、值对象、聚合、领域服务、仓储接口。
//
// 禁止：依赖任何外部框架（hertz / gorm / sonic 等）。
// 仓储实现位于 infrastructure 层。
package domain
