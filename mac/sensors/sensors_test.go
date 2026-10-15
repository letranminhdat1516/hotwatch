package sensors

import "testing"

func TestSummaryOfCPUDies(t *testing.T) {
	r := []Reading{{Name: "PMU tdie1", C: 50}, {Name: "PMU tdie2", C: 60}, {Name: "NAND CH0 temp", C: 40}, {Name: "gas gauge battery", C: 31}, {Name: "PMU tdie3", C: -1}}
	avg, hottest, ok := CPU(r)
	if !ok || avg != 55 || hottest != 60 {
		t.Fatalf("CPU = %v %v %v", avg, hottest, ok)
	}
	if _, _, ok := CPU([]Reading{{Name: "NAND CH0 temp", C: 40}}); ok {
		t.Fatal("no die sensors must report no CPU temperature")
	}
}

func TestStorageAndBatterySensors(t *testing.T) {
	r := []Reading{{"NAND CH0 temp", 43}, {"gas gauge battery", 33}, {"gas gauge battery", 31}, {"PMU tdie1", 50}, {"NAND CH1 temp", -9000}}
	if c, ok := Storage(r); !ok || c != 43 {
		t.Fatalf("storage %v %v", c, ok)
	}
	if c, ok := Battery(r); !ok || c != 32 {
		t.Fatalf("battery %v %v", c, ok)
	}
	if _, ok := Storage(nil); ok {
		t.Fatal("no sensors: unknown")
	}
}
