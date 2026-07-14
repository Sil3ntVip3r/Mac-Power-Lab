package config

import (
	"testing"
	"time"
)

func TestDefaultProcessIntervalIsLowerFrequencyThanSystemSampler(t *testing.T) {
	c := Default()
	if c.ProcessInterval <= c.PowermetricsInterval {
		t.Fatalf("process interval %s must be slower than system sampler %s", c.ProcessInterval, c.PowermetricsInterval)
	}
}

func TestRejectTooFrequentProcessSampling(t *testing.T) {
	c := Default()
	c.ProcessInterval = time.Second
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
