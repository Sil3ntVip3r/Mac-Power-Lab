package collector

import (
	"math"
	"testing"
	"time"
)

func TestParseBatteryDataUsesExplicitElectricalUnits(t *testing.T) {
	root := map[string]any{
		"ExternalConnected":       false,
		"Voltage":                 float64(12_500),
		"Amperage":                float64(50),
		"CurrentCapacity":         float64(50),
		"AppleRawCurrentCapacity": float64(5_000),
		"AppleRawMaxCapacity":     float64(8_000),
		"DesignCapacity":          float64(8_500),
		"AdapterDetails": map[string]any{
			"Name":           "140W USB-C Power Adapter",
			"Watts":          float64(140),
			"AdapterVoltage": float64(28_000),
			"Current":        float64(5_000),
		},
		"PortControllerMaxPower": float64(140_000),
	}

	battery, adapter := parseBatteryData(root, nil, "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t50%; discharging", time.Unix(1, 0))

	if battery.VoltageV != 12.5 {
		t.Fatalf("voltage=%v want=12.5", battery.VoltageV)
	}
	if battery.CurrentA != 0.05 {
		t.Fatalf("current=%v want=0.05", battery.CurrentA)
	}
	if math.Abs(battery.NetWatts-0.625) > 1e-9 {
		t.Fatalf("net watts=%v want=0.625", battery.NetWatts)
	}
	if adapter.ContractVoltageV != 28 || adapter.ContractCurrentA != 5 || adapter.ContractWatts != 140 {
		t.Fatalf("adapter=%+v", adapter)
	}
	if adapter.PortControllerMaxPowerW != 140 {
		t.Fatalf("port max=%v want=140", adapter.PortControllerMaxPowerW)
	}
}

func TestParseBatteryDataPreservesSmallNegativeCurrent(t *testing.T) {
	root := map[string]any{
		"Voltage":  float64(12_000),
		"Amperage": float64(-50),
	}
	battery, _ := parseBatteryData(root, nil, "Battery Power; discharging", time.Unix(1, 0))
	if battery.CurrentA != -0.05 {
		t.Fatalf("current=%v want=-0.05", battery.CurrentA)
	}
	if math.Abs(battery.NetWatts-(-0.6)) > 1e-9 {
		t.Fatalf("net watts=%v want=-0.6", battery.NetWatts)
	}
}

func TestPMSetDischargingIsNotCharging(t *testing.T) {
	if pmsetReportsCharging("Now drawing from 'Battery Power'\n -InternalBattery-0 50%; discharging; 2:00 remaining") {
		t.Fatal("discharging contains the substring charging but is not charging")
	}
	if !pmsetReportsCharging("Now drawing from 'AC Power'\n -InternalBattery-0 50%; charging; 1:00 remaining") {
		t.Fatal("expected charging")
	}
	if pmsetReportsCharging("Now drawing from 'AC Power'\n -InternalBattery-0 100%; charged; 0:00 remaining") {
		t.Fatal("charged battery is not actively charging")
	}
}
