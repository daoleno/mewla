package screen

import "testing"

func TestParsePMSetBattery(t *testing.T) {
	power := parsePMSetBattery("Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t87%; charging; 1:02 remaining present: true\n")
	if power.Source != "ac" || power.Percent != 87 || !power.Charging {
		t.Fatalf("AC = %+v", power)
	}
	power = parsePMSetBattery("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=4653155)\t54%; discharging; 4:10 remaining present: true\n")
	if power.Source != "battery" || power.Percent != 54 || power.Charging {
		t.Fatalf("battery = %+v", power)
	}
	if power = parsePMSetBattery("Now drawing from 'AC Power'\n"); power.Source != "ac" || power.Percent != 0 {
		t.Fatalf("desktop = %+v", power)
	}
}

func TestParseClamshell(t *testing.T) {
	if closed := parseClamshell(`    | "AppleClamshellState" = Yes`); closed == nil || !*closed {
		t.Fatal("closed lid")
	}
	if closed := parseClamshell(`    | "AppleClamshellState" = No`); closed == nil || *closed {
		t.Fatal("open lid")
	}
	if parseClamshell("") != nil {
		t.Fatal("desktop has no lid")
	}
}

func TestKeepAwakeFollowsOptIn(t *testing.T) {
	dir := t.TempDir()
	service := NewService(dir)
	if err := service.SetKeepAwake(true); err != ErrDisabled {
		t.Fatalf("keep-awake must not turn screen control on: %v", err)
	}
	if err := SaveConfig(dir, Config{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadConfig(dir)
	if !cfg.KeepAwake() {
		t.Fatal("keep-awake is on by default once screen control is enabled")
	}
	if err := service.SetKeepAwake(false); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = LoadConfig(dir); cfg.KeepAwake() || !cfg.Enabled {
		t.Fatalf("after turning keep-awake off: %+v", cfg)
	}
}
