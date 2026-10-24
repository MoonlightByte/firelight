package tests

import (
	"github.com/MoonlightByte/firelight/compiler"
	"testing"
)

// TestWornFaultInItemOrder pins which fault a world with two failing worn
// items reports: the fault of the item declared first, the one a scan of the
// items met first. Each blade's hit overflows int64 by its own constant.
func TestWornFaultInItemOrder(t *testing.T) {
	const world = `rules { transfer unequips; wear any; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "dexterity" = 9223372036854775807; }
equipment "nql-equipment-v1" {
 slot "hand" capacity 2;
 definition "gear:plus-one" named "Plus one" {
  description "Overflows by one.";
  default "one";
  mode "one" {
   occupy "hand" by 1;
   attack "cut" named "Cut" {
    hit {
     constant 1;
     term stat "dexterity" offset 0 divide 1;
    }
    damage "slashing" dice 1 sides 6 {
     constant 0;
    }
   }
  }
 }
 definition "gear:plus-two" named "Plus two" {
  description "Overflows by two.";
  default "one";
  mode "one" {
   occupy "hand" by 1;
   attack "cut" named "Cut" {
    hit {
     constant 2;
     term stat "dexterity" offset 0 divide 1;
    }
    damage "slashing" dice 1 sides 6 {
     constant 0;
    }
   }
  }
 }
}
`
	one := `item "item:one" named "One" { owner "char:a"; wearer "char:a"; definition "gear:plus-one"; mode "one"; }` + "\n"
	two := `item "item:two" named "Two" { owner "char:a"; wearer "char:a"; definition "gear:plus-two"; mode "one"; }` + "\n"
	for _, tc := range []struct{ items, actual string }{
		{one + two, "9223372036854775808"},
		{two + one, "9223372036854775809"},
	} {
		_, ds := compiler.CompileWorld("world.nql", world+tc.items)
		if len(ds) != 1 || ds[0].Code != "E_OVERFLOW" || ds[0].Actual != tc.actual {
			t.Errorf("got %#v, want one E_OVERFLOW with actual %s", ds, tc.actual)
		}
	}
}
