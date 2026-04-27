package harness

// DefaultProviders returns the built-in harness providers.
func DefaultProviders() []Provider {
	return []Provider{
		ClaudeProvider{},
		OpenCodeProvider{},
		CodexProvider{},
		CopilotProvider{},
		GeminiProvider{},
	}
}
