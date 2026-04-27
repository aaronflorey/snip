package harness

type providerDebugStats struct {
	Files        int
	Yielded      int
	ScopeSkips   int
	CutoffSkips  int
	CommandSkips int
}

func defaultString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := data[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
