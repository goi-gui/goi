package mermaid

import (
	"testing"

	"github.com/goi-gui/goi/internal/m3"
)

// TestEveryDiagramType renders one diagram of each type Pele registers: the
// component claims Mermaid's diagram set, so a type that cannot reach pixels is
// a regression here rather than a blank box in a gallery.
func TestEveryDiagramType(t *testing.T) {
	cases := map[string]string{
		"agentflow":     "agentflow-beta\nglobal\n  A\nend\nflow\n A --> B --> C\nend",
		"architecture":  "architecture-beta\nservice a\nservice b\n",
		"block":         "block-beta\ncolumns 2\nA B\n",
		"c4":            "C4Context\ntitle System\nPerson(a, \"A\")\nSystem(b, \"B\")\nRel(a,b,\"uses\")",
		"class":         "classDiagram\nclass Animal\nAnimal : +int age\nAnimal : +eat()\nclass Dog\nAnimal <|-- Dog",
		"cynefin":       "cynefin-beta\nclear\n  \"A\"\ncomplicated\n  \"B\"",
		"er":            "erDiagram\nCUSTOMER ||--o{ ORDER : places\nORDER ||--|{ LINE-ITEM : contains",
		"eventmodeling": "eventmodeling\ntf 01 ui Shop.Cart\ntf 02 cmd AddItem\ntf 03 evt ItemAdded",
		"flowchart":     "flowchart LR\n  A[Mermaid text] --> B{OK?}\n  B -->|yes| C([Done])",
		"gantt":         "gantt\ntitle A Gantt\ndateFormat YYYY-MM-DD\nsection A\nTask: 2024-01-01, 3d\nOther: after Task, 2d",
		"git":           "gitGraph\ncommit\nbranch develop\ncommit\ncheckout main\ncommit",
		"info":          "info",
		"ishikawa":      "ishikawa-beta\nProblem\nCause A\n  Subcause A1\nCause B",
		"journey":       "journey\ntitle My day\nsection Work\nMake tea: 5: Me\nDo work: 3: Me",
		"kanban":        "kanban\n  root\n    child1\n    child2",
		"mindmap":       "mindmap\n  root((Root))\n    A\n    B",
		"packet":        "packet-beta\n0-10: \"test\"\n11: \"single\"",
		"pie":           "pie title Pets\n\"Dogs\": 386\n\"Cats\": 85",
		"quadrant":      "quadrantChart\nx-axis Low --> High\ny-axis Low --> High\nA: [0.3,0.6]",
		"radar":         "radar-beta\naxis a[\"A\"], b[\"B\"]\ncurve c[\"C\"]{1,2}",
		"railroad":      "railroad-beta\nrule = sequence(terminal(\"a\"), nonterminal(\"b\")) ;",
		"requirement":   "requirementDiagram\nrequirement test_req {\nid: test_id\ntext: the test text.\nrisk: High\nverifymethod: Analysis\n}",
		"sankey":        "sankey-beta\nA,B,10\nB,C,5",
		"sequence":      "sequenceDiagram\nparticipant Alice\nparticipant Bob\nAlice->>Bob: Hello\nBob-->>Alice: Hi",
		"state":         "stateDiagram-v2\n[*] --> Still\nStill --> Moving\nMoving --> [*]",
		"swimlane":      "swimlane-beta LR\n  subgraph Customer\n    request[Request service]\n  end\n  subgraph Support\n    triage[Triage]\n  end\n  request --> triage",
		"timeline":      "timeline\nsection abc-123\n2020: A\n2021: B",
		"treemap":       "treemap-beta\n\"Root\"\n  \"A\": 10\n  \"B\": 20",
		"treeview":      "treeView-beta\nroot/\n  src/\n    index.js",
		"usecase":       "usecase-beta\ndirection LR\nactor Customer\nCheckout(\"Place order\")\nCustomer --> Checkout",
		"venn":          "venn-beta\nset A[\"A\"]\nset B[\"B\"]\ntext A \"one\"\ntext B \"two\"",
		"wardley":       "wardley-beta\ntitle W\ncomponent A [0.5,0.5]",
		"xychart":       "xychart-beta\ntitle Sales\nx-axis [jan, feb]\ny-axis 0 --> 100\nbar [10,20]",
	}
	light := m3.Light()
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	for name, src := range cases {
		data, w, h, err := eng.Render(src, light)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if data == nil || w <= 0 || h <= 0 {
			t.Errorf("%s: no diagram (data=%v w=%v h=%v)", name, data != nil, w, h)
		}
	}
}

func TestInvalidSourceIsAnError(t *testing.T) {
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if _, _, _, err := eng.Render("this is not a diagram", m3.Light()); err == nil {
		t.Fatal("garbage rendered")
	}
}
