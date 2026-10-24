package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestNativeReceiptReadAndRequestDetachment(t *testing.T) {
	b := clockFixture()
	w := gearWorld(t, b)
	original := gearRequest(w, "native-original", timed("native-guard", 60), advance(1))
	normalized := core.CanonicalRequest(original)
	*original.Operations[0].ApplyCondition.ClockDuration = 2
	receipt, e := w.Execute(normalized)
	if e != nil {
		t.Fatal(e)
	}
	if receipt.Events[0].Condition.After.ClockLifetime.ExpiresTick != 60 {
		t.Fatal("canonical request aliases input")
	}
	before := w.ExportCheckpoint()
	if empty, ok := w.Receipt("absent"); ok || !reflect.DeepEqual(empty, core.Batch{}) {
		t.Fatal("unknown receipt invented")
	}
	first, ok := w.Receipt("native-original")
	if !ok {
		t.Fatal("missing receipt")
	}
	first.Events[0].Condition.After.ClockLifetime.ExpiresTick = 99
	*first.Request.Operations[0].ApplyCondition.ClockDuration = 99
	again, ok := w.Receipt("native-original")
	if !ok || !reflect.DeepEqual(receipt, again) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("receipt aliases canonical history")
	}
	gearExecute(t, w, "expiry", advance(60))
	historical, e := w.Execute(normalized)
	if e != nil || !reflect.DeepEqual(historical, receipt) {
		t.Fatal("receipt retrieval affected retry behavior")
	}
}
