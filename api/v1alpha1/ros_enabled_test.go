package v1alpha1

import "testing"

func TestROSEnabled(t *testing.T) {
	cfg := &CostManagementServiceConfig{}
	if !ROSEnabled(cfg) {
		t.Fatal("nil Enabled should default to true")
	}
	enabled := true
	cfg.Spec.ROS.Enabled = &enabled
	if !ROSEnabled(cfg) {
		t.Fatal("Enabled=true should be true")
	}
	disabled := false
	cfg.Spec.ROS.Enabled = &disabled
	if ROSEnabled(cfg) {
		t.Fatal("Enabled=false should be false")
	}
}
