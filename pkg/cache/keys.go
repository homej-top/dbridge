package cache

// Key 生成统一命名的缓存键：Key("report", "rpt_audit_daily") => "report:rpt_audit_daily"。
// 业务层一律使用本函数（或 KeyPrefix）构造 key，避免硬编码字符串拼写错误。
func Key(module string, id string) string {
	return module + ":" + id
}

// KeyPrefix 生成前缀（用于 DelByPrefix）：KeyPrefix("report", "ds:xxx") => "report:ds:xxx:"。
// 例如批量清理某个数据源下所有报表缓存。
func KeyPrefix(module string, biz string) string {
	return module + ":" + biz + ":"
}
