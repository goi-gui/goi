package m3

import (
	"reflect"
	"testing"
	"unicode"
)

// TestEveryColorFieldHasARole keeps the role table honest: a new color role
// added to Colors but not to Role would silently be unusable from .goi.
func TestEveryColorFieldHasARole(t *testing.T) {
	c := Light().Colors
	v := reflect.ValueOf(c)
	typ := v.Type()
	seen := map[string]bool{}
	for _, name := range ColorRoles() {
		if seen[name] {
			t.Fatalf("role %q listed twice", name)
		}
		seen[name] = true
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i).Name
		role := string(unicode.ToLower(rune(field[0]))) + field[1:]
		if _, ok := c.Role(role); !ok {
			t.Errorf("Colors.%s has no role %q", field, role)
		}
	}
}
