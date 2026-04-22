// Package application 是应用层。
//
// 职责：用例编排 —— 接受 facade 传入的 DTO，组合 domain 服务完成业务流，
// 事务边界划在这里。
//
// 禁止：编写领域规则、直接操作 DB / 外部服务。
package application
