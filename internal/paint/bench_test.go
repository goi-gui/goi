package paint

import "testing"

// BenchmarkBuild1k measures building a display list for 1000 rectangles.
func BenchmarkBuild1k(b *testing.B) {
	src := "Window { Column {"
	for i := 0; i < 1000; i++ {
		src += ` Rectangle { width: 10 height: 10 background: #ff0000 }`
	}
	src += " } }"
	tr, diag := buildTree([]byte(src))
	if diag != "" {
		b.Fatal(diag)
	}
	layoutTree(tr, 1024, 768)
	l := &List{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Build(tr, 1024, 768)
	}
}
