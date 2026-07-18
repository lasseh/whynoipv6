package consensus

import "time"

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	Int(key string) int
	Float(key string) float64
	Duration(key string) time.Duration
}

// ConfigFrom binds the consensus.* registry keys (09-ops §2.3) to Config —
// the one place these key names meet their fields.
func ConfigFrom(src ConfigSource) Config {
	return Config{
		PerProviderQPS: src.Int("consensus.per_provider_qps"),
		FastLane: FastLaneConfig{
			NondefinitiveRate: src.Float("consensus.fastlane_breaker.nondefinitive_rate"),
			Window:            src.Duration("consensus.fastlane_breaker.window"),
			MinSamples:        src.Int("consensus.fastlane_breaker.min_samples"),
			RecoverBelow:      src.Float("consensus.fastlane_breaker.recover_below"),
		},
		Provider: ProviderConfig{
			FailureRate:    src.Float("consensus.provider_breaker.failure_rate"),
			Window:         src.Duration("consensus.provider_breaker.window"),
			MinSamples:     src.Int("consensus.provider_breaker.min_samples"),
			RecoveryProbes: src.Int("consensus.provider_breaker.recovery_probes"),
		},
	}
}
