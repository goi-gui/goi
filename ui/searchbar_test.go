package ui

import "testing"

// TestSearchBarSlotsTakeTheIconColumns: the leading and trailing bodies land in
// the same 40dp squares as the built-in glyphs, so a search bar with a filter
// control keeps its text box where it was.
func TestSearchBarSlotsTakeTheIconColumns(t *testing.T) {
	_, win, _ := inputFixture(t, `Window {
		Column {
			SearchBar {
				id:     search
				width:  300
				height: 56
				label:  "Search"

				$leading {
					Icon { id: lead icon: tune }
				}

				$trailing {
					Icon { id: trail icon: close }
				}
			}
		}
	}`, 400, 200)
	lead := absoluteRect(MustByID[*Icon](win, "lead"))
	trail := absoluteRect(MustByID[*Icon](win, "trail"))
	if lead.X != 16 {
		t.Fatalf("leading slot x = %v, want 16", lead.X)
	}
	if trail.X != 260 {
		t.Fatalf("trailing slot x = %v, want 260", trail.X)
	}
	if lead.Y+lead.H/2 != 28 || trail.Y+trail.H/2 != 28 {
		t.Fatalf("slot centres = %v/%v, want 28", lead.Y+lead.H/2, trail.Y+trail.H/2)
	}
}
