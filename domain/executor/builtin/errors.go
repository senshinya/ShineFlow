package builtin

import "errors"

// ErrPortNotConfigured 表示执行器依赖的 ExecServices 端口在运行时未配置。
var ErrPortNotConfigured = errors.New("required executor service port not configured")
