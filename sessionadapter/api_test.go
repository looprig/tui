package sessionadapter

// These compile-time references pin the public adapter API.
var (
	_ *Adapter
	_ = New
	_ = Restore
)
