package syntax

import "testing"

func benchmarkParse(b *testing.B, nodes int) {
	src := "Window {\n"
	for i := 0; i < nodes; i++ {
		src += `  Text { text: "hello" fontSize: 16 color: #ffffff width: 100 height: 20 }` + "\n"
	}
	src += "}\n"
	data := []byte(src)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, diag := Parse(data); !diag.Empty() {
			b.Fatal(diag)
		}
	}
}

func BenchmarkParse1k(b *testing.B)  { benchmarkParse(b, 1000) }
func BenchmarkParse10k(b *testing.B) { benchmarkParse(b, 10000) }

func BenchmarkParseError(b *testing.B) {
	data := []byte("Window {\n  width: \"x\"\n  Colum { }\n}\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Parse(data)
	}
}
